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
	vpnCmd     *exec.Cmd
	vpnCmdLock = &sync.Mutex{}
)

var vpnSocketConn net.Conn

var vpnOngoingRequests = make(map[int32]chan *CtrlResponse)

var vpnLock = &sync.Mutex{}

func CallVpnCtrlAction(action string, params map[string]interface{}) (*CtrlResponse, error) {
	vpnLock.Lock()
	ctrlAction := CtrlAction{
		Action: action,
		Seq:    nextSeq(),
		Params: params,
	}

	responseChan := make(chan *CtrlResponse, 1)
	vpnOngoingRequests[ctrlAction.Seq] = responseChan
	vpnLock.Unlock()

	jsonData, err := json.Marshal(ctrlAction)
	if err != nil {
		delete(vpnOngoingRequests, ctrlAction.Seq)
		return nil, fmt.Errorf("error marshaling ctrl action: %w", err)
	}

	scopedLogger := vpnLogger.With().
		Str("action", ctrlAction.Action).
		Interface("params", ctrlAction.Params).Logger()

	scopedLogger.Debug().Msg("sending vpn ctrl action")

	err = WriteVpnCtrlMessage(jsonData)
	if err != nil {
		delete(vpnOngoingRequests, ctrlAction.Seq)
		return nil, ErrorfL(&scopedLogger, "error writing vpn ctrl message", err)
	}

	select {
	case response := <-responseChan:
		vpnLock.Lock()
		delete(vpnOngoingRequests, ctrlAction.Seq)
		vpnLock.Unlock()
		if response.Error != "" {
			return nil, ErrorfL(
				&scopedLogger,
				"error vpn response: %s",
				errors.New(response.Error),
			)
		}
		return response, nil
	case <-time.After(30 * time.Second):
		vpnLock.Lock()
		delete(vpnOngoingRequests, ctrlAction.Seq)
		vpnLock.Unlock()
		return nil, ErrorfL(&scopedLogger, "timeout waiting for response", nil)
	}
}

func WriteVpnCtrlMessage(message []byte) error {
	if vpnSocketConn == nil {
		return fmt.Errorf("vpn socket not conn ected")
	}
	_, err := vpnSocketConn.Write(message)
	return err
}

var vpnCtrlSocketListener net.Listener

var vpnCtrlClientConnected = make(chan struct{})

func waitVpnCtrlClientConnected() {
	<-vpnCtrlClientConnected
}

func StartVpnSocketServer(socketPath string, handleClient func(net.Conn), isCtrl bool) net.Listener {
	scopedLogger := vpnLogger.With().
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
				case <-vpnCtrlClientConnected:
					scopedLogger.Debug().Msg("vpn ctrl client reconnected")
				default:
					close(vpnCtrlClientConnected)
					scopedLogger.Debug().Msg("first vpn ctrl socket client connected")
				}
			}

			//conn.Write([]byte("[handleVpnCtrlClient]vpn sock test"))
			go handleClient(conn)
		}
	}()

	return listener
}

func StartVpnCtrlSocketServer() {
	vpnCtrlSocketListener = StartVpnSocketServer("/var/run/kvm_vpn.sock", handleVpnCtrlClient, true)
	vpnLogger.Debug().Msg("vpn ctrl sock started")
}

func handleVpnCtrlClient(conn net.Conn) {
	defer conn.Close()

	scopedLogger := vpnLogger.With().
		Str("addr", conn.RemoteAddr().String()).
		Str("type", "vpn_ctrl").
		Logger()

	scopedLogger.Info().Msg("vpn socket client connected")
	if vpnSocketConn != nil {
		scopedLogger.Debug().Msg("closing existing vpn socket connection")
		vpnSocketConn.Close()
	}

	vpnSocketConn = conn

	readBuf := make([]byte, 4096)
	for {
		n, err := conn.Read(readBuf)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("error reading from vpn sock")
			break
		}

		vpnResp := CtrlResponse{}
		err = json.Unmarshal(readBuf[:n], &vpnResp)
		if err != nil {
			scopedLogger.Warn().Err(err).Str("data", string(readBuf[:n])).Msg("error parsing vpn sock msg")
			continue
		}
		scopedLogger.Trace().Interface("data", vpnResp).Msg("vpn sock msg")

		if vpnResp.Seq != 0 {
			vpnLock.Lock()
			responseChan, ok := vpnOngoingRequests[vpnResp.Seq]
			vpnLock.Unlock()
			if ok {
				select {
				case responseChan <- &vpnResp:
				default:
					scopedLogger.Warn().Int32("seq", vpnResp.Seq).Msg("dropping vpn response for completed request")
				}
			}
		}
		switch vpnResp.Event {
		case "vpn_display_update":
			HandleVpnDisplayUpdateMessage(vpnResp)
		}
	}

	scopedLogger.Debug().Msg("vpn sock disconnected")
}

func startVpnBinaryWithLock(binaryPath string) (*exec.Cmd, error) {
	vpnCmdLock.Lock()
	defer vpnCmdLock.Unlock()

	cmd, err := startVpnBinary(binaryPath)
	if err != nil {
		return nil, err
	}
	vpnCmd = cmd
	return cmd, nil
}

func restartVpnBinary(binaryPath string) error {
	time.Sleep(10 * time.Second)
	// restart the binary
	vpnLogger.Info().Msg("restarting vpn_video binary")
	cmd, err := startVpnBinary(binaryPath)
	if err != nil {
		vpnLogger.Warn().Err(err).Msg("failed to restart binary")
	}
	vpnCmd = cmd
	return err
}

func superviseVpnBinary(binaryPath string) error {
	vpnCmdLock.Lock()
	defer vpnCmdLock.Unlock()

	if vpnCmd == nil || vpnCmd.Process == nil {
		return restartVpnBinary(binaryPath)
	}

	err := vpnCmd.Wait()

	if err == nil {
		vpnLogger.Info().Err(err).Msg("kvm_vpn binary exited with no error")
	} else if exiterr, ok := err.(*exec.ExitError); ok {
		vpnLogger.Warn().Int("exit_code", exiterr.ExitCode()).Msg("kvm_vpn binary exited with error")
	} else {
		vpnLogger.Warn().Err(err).Msg("kvm_vpn binary exited with unknown error")
	}

	return restartVpnBinary(binaryPath)
}

func ExtractAndRunVpnBin() error {
	binaryPath := "/userdata/picokvm/bin/kvm_vpn"

	// Make the binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}
	// Run the binary in the background
	cmd, err := startVpnBinaryWithLock(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	// check if the binary is still running every 10 seconds
	go func() {
		for {
			select {
			case <-appCtx.Done():
				vpnLogger.Info().Msg("stopping vpn binary supervisor")
				return
			default:
				err := superviseVpnBinary(binaryPath)
				if err != nil {
					vpnLogger.Warn().Err(err).Msg("failed to supervise vpn binary")
					time.Sleep(1 * time.Second) // Add a short delay to prevent rapid successive calls
				}
			}
		}
	}()

	go func() {
		<-appCtx.Done()
		vpnLogger.Info().Int("pid", cmd.Process.Pid).Msg("killing process")
		err := cmd.Process.Kill()
		if err != nil {
			vpnLogger.Warn().Err(err).Msg("failed to kill process")
			return
		}
	}()

	vpnLogger.Info().Int("pid", cmd.Process.Pid).Msg("kvm_vpn binary started")

	return nil
}
