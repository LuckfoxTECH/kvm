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
	audioCmd     *exec.Cmd
	audioCmdLock = &sync.Mutex{}
)

var audioSocketConn net.Conn

var audioOngoingRequests = make(map[int32]chan *CtrlResponse)

var audioLock = &sync.Mutex{}

func CallAudioCtrlAction(action string, params map[string]interface{}) (*CtrlResponse, error) {
	audioLock.Lock()
	defer audioLock.Unlock()
	ctrlAction := CtrlAction{
		Action: action,
		Seq:    nextSeq(),
		Params: params,
	}

	responseChan := make(chan *CtrlResponse, 1)
	audioOngoingRequests[ctrlAction.Seq] = responseChan

	jsonData, err := json.Marshal(ctrlAction)
	if err != nil {
		delete(audioOngoingRequests, ctrlAction.Seq)
		return nil, fmt.Errorf("error marshaling ctrl action: %w", err)
	}

	scopedLogger := audioLogger.With().
		Str("action", ctrlAction.Action).
		Interface("params", ctrlAction.Params).Logger()

	scopedLogger.Debug().Msg("sending audio ctrl action")

	err = WriteAudioCtrlMessage(jsonData)
	if err != nil {
		delete(audioOngoingRequests, ctrlAction.Seq)
		return nil, ErrorfL(&scopedLogger, "error writing audio ctrl message", err)
	}

	select {
	case response := <-responseChan:
		delete(audioOngoingRequests, ctrlAction.Seq)
		if response.Error != "" {
			return nil, ErrorfL(
				&scopedLogger,
				"error audio response: %s",
				errors.New(response.Error),
			)
		}
		return response, nil
	case <-time.After(5 * time.Second):
		delete(audioOngoingRequests, ctrlAction.Seq)
		return nil, ErrorfL(&scopedLogger, "timeout waiting for response", nil)
	}
}

func WriteAudioCtrlMessage(message []byte) error {
	if audioSocketConn == nil {
		return fmt.Errorf("audio socket not connected")
	}
	_, err := audioSocketConn.Write(message)
	return err
}

var audioCtrlSocketListener net.Listener

var audioCtrlClientConnected = make(chan struct{})

func waitAudioCtrlClientConnected() {
	<-audioCtrlClientConnected
}

func StartAudioSocketServer(socketPath string, handleClient func(net.Conn), isCtrl bool) net.Listener {
	scopedLogger := audioLogger.With().
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
				case <-audioCtrlClientConnected:
					scopedLogger.Debug().Msg("audio ctrl client reconnected")
				default:
					close(audioCtrlClientConnected)
					scopedLogger.Debug().Msg("first audio ctrl socket client connected")
				}
			}

			//conn.Write([]byte("[handleAudioCtrlClient]audio sock test"))
			go handleClient(conn)
		}
	}()

	return listener
}

func StartAudioCtrlSocketServer() {
	audioCtrlSocketListener = StartAudioSocketServer("/var/run/kvm_audio.sock", handleAudioCtrlClient, true)
	audioLogger.Debug().Msg("audio ctrl sock started")
}

func handleAudioCtrlClient(conn net.Conn) {
	defer conn.Close()

	scopedLogger := audioLogger.With().
		Str("addr", conn.RemoteAddr().String()).
		Str("type", "audio_ctrl").
		Logger()

	scopedLogger.Info().Msg("audio socket client connected")
	if audioSocketConn != nil {
		scopedLogger.Debug().Msg("closing existing audio socket connection")
		audioSocketConn.Close()
	}

	audioSocketConn = conn

	readBuf := make([]byte, 4096)
	for {
		n, err := conn.Read(readBuf)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("error reading from audio sock")
			break
		}
		readMsg := string(readBuf[:n])

		audioResp := CtrlResponse{}
		err = json.Unmarshal([]byte(readMsg), &audioResp)
		if err != nil {
			scopedLogger.Warn().Err(err).Str("data", readMsg).Msg("error parsing audio sock msg")
			continue
		}
		scopedLogger.Trace().Interface("data", audioResp).Msg("audio sock msg")

		if audioResp.Seq != 0 {
			responseChan, ok := audioOngoingRequests[audioResp.Seq]
			if ok {
				responseChan <- &audioResp
			}
		}
		switch audioResp.Event {
		case "audio_input_state":
			HandleAudioStateMessage(audioResp)
		}
	}

	scopedLogger.Debug().Msg("audio sock disconnected")
}

func startAudioBinaryWithLock(binaryPath string) (*exec.Cmd, error) {
	audioCmdLock.Lock()
	defer audioCmdLock.Unlock()

	cmd, err := startAudioBinary(binaryPath)
	if err != nil {
		return nil, err
	}
	audioCmd = cmd
	return cmd, nil
}

func restartAudioBinary(binaryPath string) error {
	time.Sleep(10 * time.Second)
	// restart the binary
	audioLogger.Info().Msg("restarting audio_video binary")
	cmd, err := startAudioBinary(binaryPath)
	if err != nil {
		audioLogger.Warn().Err(err).Msg("failed to restart binary")
	}
	audioCmd = cmd
	return err
}

func superviseAudioBinary(binaryPath string) error {
	audioCmdLock.Lock()
	defer audioCmdLock.Unlock()

	if audioCmd == nil || audioCmd.Process == nil {
		return restartAudioBinary(binaryPath)
	}

	err := audioCmd.Wait()

	if err == nil {
		audioLogger.Info().Err(err).Msg("kvm_audio binary exited with no error")
	} else if exiterr, ok := err.(*exec.ExitError); ok {
		audioLogger.Warn().Int("exit_code", exiterr.ExitCode()).Msg("kvm_audio binary exited with error")
	} else {
		audioLogger.Warn().Err(err).Msg("kvm_audio binary exited with unknown error")
	}

	return restartAudioBinary(binaryPath)
}

func ExtractAndRunAudioBin() error {
	binaryPath := "/userdata/picokvm/bin/kvm_audio"

	// Make the binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}
	// Run the binary in the background
	cmd, err := startAudioBinaryWithLock(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	// check if the binary is still running every 10 seconds
	go func() {
		for {
			select {
			case <-appCtx.Done():
				audioLogger.Info().Msg("stopping audio binary supervisor")
				return
			default:
				err := superviseAudioBinary(binaryPath)
				if err != nil {
					audioLogger.Warn().Err(err).Msg("failed to supervise audio binary")
					time.Sleep(1 * time.Second) // Add a short delay to prevent rapid successive calls
				}
			}
		}
	}()

	go func() {
		<-appCtx.Done()
		audioLogger.Info().Int("pid", cmd.Process.Pid).Msg("killing process")
		err := cmd.Process.Kill()
		if err != nil {
			audioLogger.Warn().Err(err).Msg("failed to kill process")
			return
		}
	}()

	audioLogger.Info().Int("pid", cmd.Process.Pid).Msg("kvm_audio binary started")

	return nil
}
