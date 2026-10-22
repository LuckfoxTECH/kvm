package kvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

var (
	displayCmd     *exec.Cmd
	displayCmdLock = &sync.Mutex{}
)

var displaySocketConn net.Conn

var (
	displayOngoingRequests     = make(map[int32]chan *CtrlResponse)
	displayOngoingRequestsLock = &sync.Mutex{}
	displayConnWriteLock       = &sync.Mutex{}
)

var displayLock = &sync.Mutex{}

func CallDisplayCtrlAction(action string, params map[string]interface{}) (*CtrlResponse, error) {
	displayLock.Lock()
	defer displayLock.Unlock()

	// The sequence number has to be reserved and registered atomically: the display
	// socket handler looks the channel up from its own goroutine.
	responseChan := make(chan *CtrlResponse, 1)
	ctrlSeq := nextSeq()
	displayOngoingRequestsLock.Lock()
	displayOngoingRequests[ctrlSeq] = responseChan
	displayOngoingRequestsLock.Unlock()

	ctrlAction := CtrlAction{
		Action: action,
		Seq:    ctrlSeq,
		Params: params,
	}

	jsonData, err := json.Marshal(ctrlAction)
	if err != nil {
		dropDisplayResponseChan(ctrlSeq)
		return nil, fmt.Errorf("error marshaling ctrl action: %w", err)
	}

	scopedLogger := displayLogger.With().
		Str("action", ctrlAction.Action).
		Interface("params", ctrlAction.Params).Logger()

	scopedLogger.Debug().Msg("sending display ctrl action")

	err = WriteDisplayCtrlMessage(jsonData)
	if err != nil {
		dropDisplayResponseChan(ctrlSeq)
		return nil, ErrorfL(&scopedLogger, "error writing display ctrl message", err)
	}

	select {
	case response := <-responseChan:
		dropDisplayResponseChan(ctrlSeq)
		if response.Error != "" {
			return nil, ErrorfL(
				&scopedLogger,
				"error display response: %s",
				errors.New(response.Error),
			)
		}
		return response, nil
	case <-time.After(3 * time.Second):
		// Never close the channel here: a late response must be dropped, not sent
		// on a closed channel (that panics the whole app).
		dropDisplayResponseChan(ctrlSeq)
		return nil, ErrorfL(&scopedLogger, "timeout waiting for response", nil)
	}
}

// dropDisplayResponseChan unregisters a pending request, any response that arrives
// later is discarded by the socket handler.
func dropDisplayResponseChan(ctrlSeq int32) {
	displayOngoingRequestsLock.Lock()
	delete(displayOngoingRequests, ctrlSeq)
	displayOngoingRequestsLock.Unlock()
}

// deliverDisplayResponse hands a parsed response to the goroutine waiting for it. The
// channel is buffered so the socket handler never blocks and never panics.
func deliverDisplayResponse(ctrlSeq int32, response *CtrlResponse) {
	displayOngoingRequestsLock.Lock()
	defer displayOngoingRequestsLock.Unlock()

	responseChan, ok := displayOngoingRequests[ctrlSeq]
	if !ok {
		return
	}

	select {
	case responseChan <- response:
	default:
	}
}

func WriteDisplayCtrlMessage(message []byte) error {
	displayConnWriteLock.Lock()
	defer displayConnWriteLock.Unlock()

	if displaySocketConn == nil {
		return fmt.Errorf("display socket not connected")
	}

	_, err := displaySocketConn.Write(message)
	return err
}

var displayCtrlSocketListener net.Listener

var displayCtrlClientConnected = make(chan struct{})

func waitDisplayCtrlClientConnected() {
	<-displayCtrlClientConnected
}

func StartDisplaySocketServer(socketPath string, handleClient func(net.Conn), isCtrl bool) net.Listener {
	scopedLogger := displayLogger.With().
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
			conn, err := listener.Accept()

			if err != nil {
				scopedLogger.Warn().Err(err).Msg("failed to accept socket")
				continue
			}
			if isCtrl {
				// check if the channel is closed
				select {
				case <-displayCtrlClientConnected:
					scopedLogger.Debug().Msg("display ctrl client reconnected")
				default:
					close(displayCtrlClientConnected)
					scopedLogger.Debug().Msg("first display ctrl socket client connected")
				}
			}

			//conn.Write([]byte("[handleDisplayCtrlClient]display sock test"))
			go handleClient(conn)
		}
	}()

	return listener
}

func StartDisplayCtrlSocketServer() {
	displayCtrlSocketListener = StartDisplaySocketServer("/var/run/kvm_display.sock", handleDisplayCtrlClient, true)
	displayLogger.Debug().Msg("display ctrl sock started")
}

func handleDisplayCtrlClient(conn net.Conn) {
	defer conn.Close()

	scopedLogger := displayLogger.With().
		Str("addr", conn.RemoteAddr().String()).
		Str("type", "display_ctrl").
		Logger()

	scopedLogger.Info().Msg("display socket client connected")
	if displaySocketConn != nil {
		scopedLogger.Debug().Msg("closing existing display socket connection")
		displaySocketConn.Close()
	}

	displaySocketConn = conn

	readBuf := make([]byte, 4096)
	for {
		n, err := conn.Read(readBuf)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("error reading from display sock")
			break
		}

		displayResp := CtrlResponse{}
		err = json.Unmarshal(readBuf[:n], &displayResp)
		if err != nil {
			scopedLogger.Warn().Err(err).Str("data", string(readBuf[:n])).Msg("error parsing display sock msg")
			continue
		}
		scopedLogger.Trace().Interface("data", displayResp).Msg("display sock msg")

		if displayResp.Seq != 0 {
			response := displayResp
			deliverDisplayResponse(response.Seq, &response)
		}

		// A response is never an event, dispatching it as one would feed it into
		// HandleDisplayActionMessage as an unknown action.
		if displayResp.Event == "" {
			continue
		}

		switch displayResp.Event {
		case "display_action":
			HandleDisplayActionMessage(displayResp)
		}
	}

	scopedLogger.Debug().Msg("display sock disconnected")
}

func startDisplayBinaryWithLock(binaryPath string) (*exec.Cmd, error) {
	displayCmdLock.Lock()
	defer displayCmdLock.Unlock()

	cmd, err := startDisplayBinary(binaryPath)
	if err != nil {
		return nil, err
	}
	displayCmd = cmd
	return cmd, nil
}

func restartDisplayBinary(binaryPath string) error {
	time.Sleep(10 * time.Second)
	// restart the binary
	displayLogger.Info().Msg("restarting display_video binary")
	cmd, err := startDisplayBinary(binaryPath)
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to restart binary")
	}
	displayCmd = cmd
	return err
}

func superviseDisplayBinary(binaryPath string) error {
	displayCmdLock.Lock()
	defer displayCmdLock.Unlock()

	if displayCmd == nil || displayCmd.Process == nil {
		return restartDisplayBinary(binaryPath)
	}

	err := displayCmd.Wait()

	if err == nil {
		displayLogger.Info().Err(err).Msg("kvm_display binary exited with no error")
	} else if exiterr, ok := err.(*exec.ExitError); ok {
		displayLogger.Warn().Int("exit_code", exiterr.ExitCode()).Msg("kvm_display binary exited with error")
	} else {
		displayLogger.Warn().Err(err).Msg("kvm_display binary exited with unknown error")
	}

	return restartDisplayBinary(binaryPath)
}

func ExtractAndRunDisplayBin() error {
	binaryPath := "/userdata/picokvm/bin/kvm_display"

	// Make the binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}
	// Run the binary in the background
	cmd, err := startDisplayBinaryWithLock(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	// check if the binary is still running every 10 seconds
	go func() {
		for {
			select {
			case <-appCtx.Done():
				displayLogger.Info().Msg("stopping display binary supervisor")
				return
			default:
				err := superviseDisplayBinary(binaryPath)
				if err != nil {
					displayLogger.Warn().Err(err).Msg("failed to supervise display binary")
					time.Sleep(1 * time.Second) // Add a short delay to prevent rapid successive calls
				}
			}
		}
	}()

	go func() {
		<-appCtx.Done()
		displayLogger.Info().Int("pid", cmd.Process.Pid).Msg("killing process")
		err := cmd.Process.Kill()
		if err != nil {
			displayLogger.Warn().Err(err).Msg("failed to kill process")
			return
		}
	}()

	displayLogger.Info().Int("pid", cmd.Process.Pid).Msg("kvm_display binary started")

	return nil
}
