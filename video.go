package kvm

import (
	"encoding/json"
)

// max frame size for 1080p video, specified in mpp venc setting
const maxFrameSize = 1920 * 1080 / 2

func writeCtrlAction(action string) error {
	actionMessage := map[string]string{
		"action": action,
	}
	jsonMessage, err := json.Marshal(actionMessage)
	if err != nil {
		return err
	}
	err = WriteCtrlMessage(jsonMessage)
	return err
}

// stopVideo stops the encoder. kvm_video's snapshot thread exits with the
// encoder and is never created again, so after the next start_video every
// jpeg_take_snapshot times out. jpeg_capture_stop joins the exited thread so
// the next snapshot creates a new one. Only send it after stop_video: while the
// thread is still running it blocks kvm_video's control loop.
func stopVideo() {
	if err := writeCtrlAction("stop_video"); err != nil {
		videoLogger.Warn().Err(err).Msg("failed to stop video")
		return
	}
	if _, err := CallCtrlAction("jpeg_capture_stop", nil); err != nil {
		videoLogger.Warn().Err(err).Msg("failed to reset the JPEG snapshot thread")
	}
}

type VideoInputState struct {
	Ready          bool    `json:"ready"`
	Error          string  `json:"error,omitempty"` // no_signal, no_lock, out_of_range
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"frame_per_second"`
}

var lastVideoState VideoInputState

func triggerVideoStateUpdate() {
	go func() {
		writeJSONRPCEvent("videoInputState", lastVideoState, currentSession)
	}()
}

func HandleVideoStateMessage(event CtrlResponse) {
	videoState := VideoInputState{}
	err := json.Unmarshal(event.Data, &videoState)
	if err != nil {
		logger.Warn().Err(err).Msg("Error parsing video state json")
		return
	}
	lastVideoState = videoState
	triggerVideoStateUpdate()
	requestDisplayUpdate(true)
}

func rpcGetVideoState() (VideoInputState, error) {
	return lastVideoState, nil
}

func setForceHpd() error {
	err := rpcSetForceHpd(config.ForceHpd)
	return err
}

func setNpuAppStatus() error {
	err := rpcSetNpuAppStatus(config.NpuAppEnabled)
	return err
}
