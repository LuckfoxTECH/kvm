package kvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
)

var ctrlSocketConn net.Conn

// The EDID write makes the host re-read the descriptor, which can trigger a
// re-enumeration. It has to run once per boot, not once per ctrl-socket
// reconnect, so it is gated on an explicit success flag rather than sync.Once:
// a connection that dies while the retries are still running should leave the
// job pending for the next connection instead of marking it done.
var (
	edidRestoreMu sync.Mutex
	edidRestored  bool
)

type CtrlAction struct {
	Action string                 `json:"action"`
	Seq    int32                  `json:"seq,omitempty"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type CtrlResponse struct {
	Seq    int32                  `json:"seq,omitempty"`
	Error  string                 `json:"error,omitempty"`
	Errno  int32                  `json:"errno,omitempty"`
	Result map[string]interface{} `json:"result,omitempty"`
	Event  string                 `json:"event,omitempty"`
	Data   json.RawMessage        `json:"data,omitempty"`
}

type EventHandler func(event CtrlResponse)

var seq int32 = 1

// nextSeq hands out the next control request sequence number. It is atomic because
// several control sockets reserve sequence numbers from concurrent goroutines.
func nextSeq() int32 {
	return atomic.AddInt32(&seq, 1)
}

var ongoingRequests = make(map[int32]chan *CtrlResponse)

var lock = &sync.Mutex{}

var (
	videoCmd     *exec.Cmd
	videoCmdLock = &sync.Mutex{}
)

// jpegReadyCh is written by the ctrl socket event handler and read by captureScreenshot.
// Buffered to 1 so the jpeg thread doesn't block if captureScreenshot has already timed out.
var jpegReadyCh = make(chan struct{}, 1)

// jpegFailedCh carries the reason a pending snapshot can never complete. Native sends
// it from every path that would otherwise stay silent: encoder not running,
// VENC_GetStream giving up, unwritable spool file, uninitialised JPEG channel.
var jpegFailedCh = make(chan string, 1)

func CallCtrlAction(action string, params map[string]interface{}) (*CtrlResponse, error) {
	lock.Lock()
	defer lock.Unlock()
	ctrlAction := CtrlAction{
		Action: action,
		Seq:    nextSeq(),
		Params: params,
	}

	responseChan := make(chan *CtrlResponse, 1)
	ongoingRequests[ctrlAction.Seq] = responseChan

	jsonData, err := json.Marshal(ctrlAction)
	if err != nil {
		delete(ongoingRequests, ctrlAction.Seq)
		return nil, fmt.Errorf("error marshaling ctrl action: %w", err)
	}

	scopedLogger := videoLogger.With().
		Str("action", ctrlAction.Action).
		Interface("params", ctrlAction.Params).Logger()

	scopedLogger.Debug().Msg("sending ctrl action")

	err = WriteCtrlMessage(jsonData)
	if err != nil {
		delete(ongoingRequests, ctrlAction.Seq)
		return nil, ErrorfL(&scopedLogger, "error writing ctrl message", err)
	}

	select {
	case response := <-responseChan:
		delete(ongoingRequests, ctrlAction.Seq)
		if response.Error != "" {
			return nil, ErrorfL(
				&scopedLogger,
				"error native response: %s",
				errors.New(response.Error),
			)
		}
		return response, nil
	case <-time.After(5 * time.Second):
		close(responseChan)
		delete(ongoingRequests, ctrlAction.Seq)
		return nil, ErrorfL(&scopedLogger, "timeout waiting for response", nil)
	}
}

func WriteCtrlMessage(message []byte) error {
	if ctrlSocketConn == nil {
		return fmt.Errorf("ctrl socket not conn ected")
	}
	_, err := ctrlSocketConn.Write(message)
	return err
}

var videoCtrlSocketListener net.Listener //nolint:unused
var videoSocketListener net.Listener     //nolint:unused

var ctrlClientConnected = make(chan struct{})

func waitCtrlClientConnected() {
	<-ctrlClientConnected
}

func StartVideoSocketServer(socketPath string, handleClient func(net.Conn), isCtrl bool) net.Listener {
	scopedLogger := videoLogger.With().
		Str("socket_path", socketPath).
		Logger()

	// Remove the socket file if it already exists
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			scopedLogger.Warn().Err(err).Msg("failed to remove existing socket file")
			os.Exit(1)
		}
	}

	listener, err := net.Listen("unixpacket", socketPath)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to start server")
		os.Exit(1)
	}

	scopedLogger.Info().Msg("server listening")

	go func() {
		for {
			scopedLogger.Debug().Msg("waiting for client connection")
			conn, err := listener.Accept()

			if err != nil {
				scopedLogger.Warn().Err(err).Msg("failed to accept socket")
				continue
			}
			scopedLogger.Info().Str("remote_addr", conn.RemoteAddr().String()).Msg("new client connection accepted")
			if isCtrl {
				// check if the channel is closed
				select {
				case <-ctrlClientConnected:
					scopedLogger.Debug().Msg("ctrl client reconnected")
				default:
					close(ctrlClientConnected)
					scopedLogger.Debug().Msg("first native ctrl socket client connected")
				}
			}

			go handleClient(conn)
		}
	}()

	return listener
}

func StartVideoCtrlSocketServer() {
	videoCtrlSocketListener = StartVideoSocketServer("/var/run/kvm_ctrl.sock", handleCtrlClient, true)
	videoLogger.Debug().Msg("native app ctrl sock started")
}

func StartVideoDataSocketServer() {
	videoSocketListener = StartVideoSocketServer("/var/run/kvm_video.sock", handleVideoClient, false)
	videoLogger.Debug().Msg("native app video sock started")
}

func handleCtrlClient(conn net.Conn) {
	defer conn.Close()

	scopedLogger := videoLogger.With().
		Str("addr", conn.RemoteAddr().String()).
		Str("type", "ctrl").
		Logger()

	scopedLogger.Info().Msg("native ctrl socket client connected")
	if ctrlSocketConn != nil {
		scopedLogger.Debug().Msg("closing existing native socket connection")
		ctrlSocketConn.Close()
	}

	ctrlSocketConn = conn

	// Restore HDMI EDID if applicable. Guarded so a reconnect does not re-apply it.
	go restoreHdmiEdidOnce()

	readBuf := make([]byte, 4096)
	for {
		n, err := conn.Read(readBuf)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("error reading from ctrl sock")
			break
		}

		ctrlResp := CtrlResponse{}
		err = json.Unmarshal(readBuf[:n], &ctrlResp)
		if err != nil {
			scopedLogger.Warn().Err(err).Str("data", string(readBuf[:n])).Msg("error parsing ctrl sock msg")
			continue
		}
		scopedLogger.Trace().Interface("data", ctrlResp).Msg("ctrl sock msg")

		if ctrlResp.Seq != 0 {
			responseChan, ok := ongoingRequests[ctrlResp.Seq]
			if ok {
				responseChan <- &ctrlResp
			}
		}
		switch ctrlResp.Event {
		case "video_input_state":
			HandleVideoStateMessage(ctrlResp)
		case "jpeg_ready":
			select {
			case jpegReadyCh <- struct{}{}:
			default:
			}
		case "jpeg_failed":
			reason := "native reported a failed capture"
			if ctrlResp.Data != nil {
				var payload struct {
					Error string `json:"error"`
				}
				if json.Unmarshal(ctrlResp.Data, &payload) == nil && payload.Error != "" {
					reason = payload.Error
				}
			}
			select {
			case jpegFailedCh <- reason:
			default:
			}
		}
	}

	scopedLogger.Debug().Msg("ctrl sock disconnected")
}

func handleVideoClient(conn net.Conn) {
	defer conn.Close()

	scopedLogger := videoLogger.With().
		Str("addr", conn.RemoteAddr().String()).
		Str("type", "video").
		Logger()

	scopedLogger.Info().Msg("native video socket client connected")

	inboundPacket := make([]byte, maxFrameSize)
	lastFrame := time.Now()
	for {
		n, err := conn.Read(inboundPacket)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("error during read")
			return
		}
		now := time.Now()
		sinceLastFrame := now.Sub(lastFrame)
		lastFrame = now

		// Broadcast to HTTP clients
		videoBroadcaster.Broadcast(inboundPacket[:n])

		if currentSession != nil {
			err := currentSession.VideoTrack.WriteSample(media.Sample{Data: inboundPacket[:n], Duration: sinceLastFrame})
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("error writing sample")
			}
		}
	}
}

func startVideoBinaryWithLock(binaryPath string) (*exec.Cmd, error) {
	videoCmdLock.Lock()
	defer videoCmdLock.Unlock()

	cmd, err := startVideoBinary(binaryPath)
	if err != nil {
		return nil, err
	}
	videoCmd = cmd
	return cmd, nil
}

func restartVideoBinary(binaryPath string) error {
	time.Sleep(10 * time.Second)
	// restart the binary
	videoLogger.Info().Msg("restarting kvm_video binary")
	cmd, err := startVideoBinary(binaryPath)
	if err != nil {
		videoLogger.Warn().Err(err).Msg("failed to restart binary")
	}
	videoCmd = cmd
	return err
}

func superviseVideoBinary(binaryPath string) error {
	videoCmdLock.Lock()
	defer videoCmdLock.Unlock()

	if videoCmd == nil || videoCmd.Process == nil {
		return restartVideoBinary(binaryPath)
	}

	err := videoCmd.Wait()

	if err == nil {
		videoLogger.Info().Err(err).Msg("kvm_video binary exited with no error")
	} else if exiterr, ok := err.(*exec.ExitError); ok {
		videoLogger.Warn().Int("exit_code", exiterr.ExitCode()).Msg("kvm_video binary exited with error")
	} else {
		videoLogger.Warn().Err(err).Msg("kvm_video binary exited with unknown error")
	}

	return restartVideoBinary(binaryPath)
}

func ExtractAndRunVideoBin() error {
	binaryPath := "/userdata/picokvm/bin/kvm_video"

	// Make the binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}
	// Run the binary in the background
	cmd, err := startVideoBinaryWithLock(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	// check if the binary is still running every 10 seconds
	go func() {
		for {
			select {
			case <-appCtx.Done():
				videoLogger.Info().Msg("stopping native binary supervisor")
				return
			default:
				err := superviseVideoBinary(binaryPath)
				if err != nil {
					videoLogger.Warn().Err(err).Msg("failed to supervise native binary")
					time.Sleep(1 * time.Second) // Add a short delay to prevent rapid successive calls
				}
			}
		}
	}()

	go func() {
		<-appCtx.Done()
		videoLogger.Info().Int("pid", cmd.Process.Pid).Msg("killing process")
		err := cmd.Process.Kill()
		if err != nil {
			videoLogger.Warn().Err(err).Msg("failed to kill process")
			return
		}
	}()

	videoLogger.Info().Int("pid", cmd.Process.Pid).Msg("kvm_video binary started")

	return nil
}

// Restore the HDMI EDID from the config. The app is the source of truth: the
// controller may ship its own EDID, and it is overwritten here so the configured
// value always wins. On first boot there is no configured value yet, so the
// built-in default is written to the config and used from then on.
func restoreHdmiEdid() bool {
	edid := config.EdidString
	if edid == "" {
		edid = defaultEdidHex
		config.EdidString = edid
		if err := SaveConfig(); err != nil {
			videoLogger.Warn().Err(err).Msg("Failed to persist default HDMI EDID")
		} else {
			videoLogger.Info().Msg("Persisted default HDMI EDID to config")
		}
	}

	// The controller may not be ready right after the socket connects.
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := CallCtrlAction("set_edid", map[string]interface{}{"edid": edid})
		if err == nil {
			videoLogger.Info().Int("attempt", attempt).Msg("Applied HDMI EDID from config")
			return true
		}
		videoLogger.Warn().Err(err).Int("attempt", attempt).Msg("Failed to apply HDMI EDID")
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	videoLogger.Error().Msg("Giving up on applying HDMI EDID from config")
	return false
}

// restoreHdmiEdidOnce applies the configured EDID at most once per boot.
func restoreHdmiEdidOnce() {
	edidRestoreMu.Lock()
	defer edidRestoreMu.Unlock()

	if edidRestored {
		return
	}
	if restoreHdmiEdid() {
		edidRestored = true
	}
}
