package kvm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"go.bug.st/serial"

	"kvm/internal/hidrpc"
	"kvm/internal/mcp"
	"kvm/internal/usbgadget"
)

type JSONRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
	ID      interface{}            `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type JSONRPCEvent struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

func isJSONRPCNotification(request JSONRPCRequest) bool {
	return request.ID == nil
}

type DisplayRotationSettings struct {
	Rotation string `json:"rotation"`
}

type BacklightSettings struct {
	MaxBrightness int `json:"max_brightness"`
	DimAfter      int `json:"dim_after"`
	OffAfter      int `json:"off_after"`
}

func writeJSONRPCResponse(response JSONRPCResponse, session *Session) {
	responseBytes, err := json.Marshal(response)
	if err != nil {
		jsonRpcLogger.Warn().Err(err).Msg("Error marshalling JSONRPC response")
		return
	}
	err = session.RPCChannel.SendText(string(responseBytes))
	if err != nil {
		jsonRpcLogger.Warn().Err(err).Msg("Error sending JSONRPC response")
		return
	}
}

func writeJSONRPCEvent(event string, params interface{}, session *Session) {
	request := JSONRPCEvent{
		JSONRPC: "2.0",
		Method:  event,
		Params:  params,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		jsonRpcLogger.Warn().Err(err).Msg("Error marshalling JSONRPC event")
		return
	}
	if session == nil || session.RPCChannel == nil {
		// Startup broadcasts reach here once per event type before a client
		// connects, so INFO floods the log with a line per event.
		jsonRpcLogger.Debug().Msg("RPC channel not available")
		return
	}

	requestString := string(requestBytes)

	jsonRpcLogger.Trace().Str("event", event).Msg("sending JSONRPC event")

	err = session.RPCChannel.SendText(requestString)
	if err != nil {
		jsonRpcLogger.Warn().Err(err).Str("event", event).Msg("error sending JSONRPC event")
		return
	}
}

func DispatchRPCRequest(request JSONRPCRequest) (JSONRPCResponse, error) {
	// A keyboard report from the Web UI is one writer among several: without the
	// input lease it could land in the middle of a string typed by an MCP tool
	// and scramble the text on the remote host. keypressReport is the same kind
	// of writer and must hold the same lease.
	if request.Method == "keyboardReport" || request.Method == "keypressReport" {
		release, err := mcp.AcquireInput(mcp.OwnerWeb, mcp.DefaultInputHold)
		if err != nil {
			return JSONRPCResponse{
				JSONRPC: "2.0",
				Error: map[string]interface{}{
					"code":    -32000,
					"message": err.Error(),
				},
				ID: request.ID,
			}, nil
		}
		defer release()
	}

	handler, ok := rpcHandlers[request.Method]
	if !ok {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			Error: map[string]interface{}{
				"code":    -32601,
				"message": "Method not found",
			},
			ID: request.ID,
		}, nil
	}

	result, err := callRPCHandler(handler, request.Params)
	if err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			Error: map[string]interface{}{
				"code":    -32603,
				"message": "Internal error",
				"data":    err.Error(),
			},
			ID: request.ID,
		}, nil
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      request.ID,
	}, nil
}

func onRPCMessage(message webrtc.DataChannelMessage, session *Session) {
	var request JSONRPCRequest
	err := json.Unmarshal(message.Data, &request)
	if err != nil {
		jsonRpcLogger.Warn().
			Str("data", string(message.Data)).
			Err(err).
			Msg("Error unmarshalling JSONRPC request")

		errorResponse := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: map[string]interface{}{
				"code":    -32700,
				"message": "Parse error",
			},
			ID: 0,
		}
		writeJSONRPCResponse(errorResponse, session)
		return
	}

	scopedLogger := jsonRpcLogger.With().
		Str("method", request.Method).
		Interface("params", request.Params).
		Interface("id", request.ID).Logger()

	scopedLogger.Trace().Msg("Received RPC request")

	response, _ := DispatchRPCRequest(request)

	if isJSONRPCNotification(request) {
		return
	}

	scopedLogger.Trace().Interface("result", response.Result).Msg("RPC handler returned")

	writeJSONRPCResponse(response, session)
}

func rpcPing() (string, error) {
	return "pong", nil
}

type BootStorageTypeResponse struct {
	Type string `json:"type"`
}

func rpcGetLocalPackageInfo() (*LocalPackageInfo, error) {
	return GetLocalPackageInfo()
}

func rpcClearLocalPackage() error {
	otaUploadMutex.Lock()
	defer otaUploadMutex.Unlock()

	if otaState.Updating {
		return fmt.Errorf("update already in progress")
	}

	cleanupLocalPackage()
	return nil
}

func rpcGetBootStorageType() (*BootStorageTypeResponse, error) {
	return &BootStorageTypeResponse{
		Type: string(GetBootStorageType()),
	}, nil
}

func rpcGetDeviceID() (string, error) {
	return GetDeviceID(), nil
}

func rpcReboot(force bool) error {
	logger.Info().Msg("Got reboot request from JSONRPC, rebooting...")

	args := []string{}
	if force {
		args = append(args, "-f")
	}

	cmd := exec.Command("reboot", args...)
	err := cmd.Start()
	if err != nil {
		logger.Error().Err(err).Msg("failed to reboot")
		return fmt.Errorf("failed to reboot: %w", err)
	}

	// If the reboot command is successful, exit the program after 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}()

	return nil
}

var streamFactor = 1.0

func rpcGetStreamQualityFactor() (float64, error) {
	return streamFactor, nil
}

func rpcSetStreamQualityFactor(factor float64) error {
	logger.Info().Float64("factor", factor).Msg("Setting stream quality factor")
	var _, err = CallCtrlAction("set_video_quality_factor", map[string]interface{}{"quality_factor": factor})
	if err != nil {
		return err
	}

	streamFactor = factor
	return nil
}

var streamEncodecType = "avc"

func rpcGetStreamEncodecType() (string, error) {
	return streamEncodecType, nil
}

func rpcSetStreamEncodecType(encodecType string) error {
	logger.Info().Str("encodecType", encodecType).Msg("Setting stream encodec type")
	var _, err = CallCtrlAction("set_video_encodec_type", map[string]interface{}{"encodec_type": encodecType})
	if err != nil {
		return err
	}

	streamEncodecType = encodecType
	return nil
}

type RcQpParams struct {
	S32FirstFrameStartQp       int `json:"s32FirstFrameStartQp"`
	U32StepQp                  int `json:"u32StepQp"`
	U32MinQp                   int `json:"u32MinQp"`
	U32MaxQp                   int `json:"u32MaxQp"`
	U32MinIQp                  int `json:"u32MinIQp"`
	U32MaxIQp                  int `json:"u32MaxIQp"`
	S32DeltIpQp                int `json:"s32DeltIpQp"`
	S32MaxReEncodeTimes        int `json:"s32MaxReEncodeTimes"`
	U32FrmMaxQp                int `json:"u32FrmMaxQp"`
	U32FrmMinQp                int `json:"u32FrmMinQp"`
	U32FrmMinIQp               int `json:"u32FrmMinIQp"`
	U32FrmMaxIQp               int `json:"u32FrmMaxIQp"`
	U32MotionStaticSwitchFrmQp int `json:"u32MotionStaticSwitchFrmQp"`
}

type VideoRcConfigParams struct {
	H264 RcQpParams `json:"h264"`
	H265 RcQpParams `json:"h265"`
}

func rpcSetVideoRc(params VideoRcConfigParams) error {
	logger.Info().Interface("params", params).Msg("Setting video RC params")
	rcParams := map[string]interface{}{
		"h264": map[string]interface{}{
			"s32FirstFrameStartQp":       params.H264.S32FirstFrameStartQp,
			"u32StepQp":                  params.H264.U32StepQp,
			"u32MinQp":                   params.H264.U32MinQp,
			"u32MaxQp":                   params.H264.U32MaxQp,
			"u32MinIQp":                  params.H264.U32MinIQp,
			"u32MaxIQp":                  params.H264.U32MaxIQp,
			"s32DeltIpQp":                params.H264.S32DeltIpQp,
			"s32MaxReEncodeTimes":        params.H264.S32MaxReEncodeTimes,
			"u32FrmMaxQp":                params.H264.U32FrmMaxQp,
			"u32FrmMinQp":                params.H264.U32FrmMinQp,
			"u32FrmMinIQp":               params.H264.U32FrmMinIQp,
			"u32FrmMaxIQp":               params.H264.U32FrmMaxIQp,
			"u32MotionStaticSwitchFrmQp": params.H264.U32MotionStaticSwitchFrmQp,
		},
		"h265": map[string]interface{}{
			"s32FirstFrameStartQp":       params.H265.S32FirstFrameStartQp,
			"u32StepQp":                  params.H265.U32StepQp,
			"u32MinQp":                   params.H265.U32MinQp,
			"u32MaxQp":                   params.H265.U32MaxQp,
			"u32MinIQp":                  params.H265.U32MinIQp,
			"u32MaxIQp":                  params.H265.U32MaxIQp,
			"s32DeltIpQp":                params.H265.S32DeltIpQp,
			"s32MaxReEncodeTimes":        params.H265.S32MaxReEncodeTimes,
			"u32FrmMaxQp":                params.H265.U32FrmMaxQp,
			"u32FrmMinQp":                params.H265.U32FrmMinQp,
			"u32FrmMinIQp":               params.H265.U32FrmMinIQp,
			"u32FrmMaxIQp":               params.H265.U32FrmMaxIQp,
			"u32MotionStaticSwitchFrmQp": params.H265.U32MotionStaticSwitchFrmQp,
		},
	}
	var _, err = CallCtrlAction("set_video_rc", rcParams)
	return err
}

func rpcGetVideoRc() (VideoRcConfigParams, error) {
	resp, err := CallCtrlAction("get_video_rc", nil)
	if err != nil {
		return VideoRcConfigParams{}, err
	}

	result := resp.Result
	if result == nil {
		return VideoRcConfigParams{}, errors.New("invalid response format")
	}

	h264Map, _ := result["h264"].(map[string]interface{})
	h265Map, _ := result["h265"].(map[string]interface{})

	getInt := func(m map[string]interface{}, k string) int {
		if v, ok := m[k].(float64); ok {
			return int(v)
		}
		return 0
	}
	getUint := func(m map[string]interface{}, k string) int {
		return getInt(m, k)
	}

	rc := VideoRcConfigParams{
		H264: RcQpParams{
			S32FirstFrameStartQp:       getInt(h264Map, "s32FirstFrameStartQp"),
			U32StepQp:                  getUint(h264Map, "u32StepQp"),
			U32MinQp:                   getUint(h264Map, "u32MinQp"),
			U32MaxQp:                   getUint(h264Map, "u32MaxQp"),
			U32MinIQp:                  getUint(h264Map, "u32MinIQp"),
			U32MaxIQp:                  getUint(h264Map, "u32MaxIQp"),
			S32DeltIpQp:                getInt(h264Map, "s32DeltIpQp"),
			S32MaxReEncodeTimes:        getInt(h264Map, "s32MaxReEncodeTimes"),
			U32FrmMaxQp:                getUint(h264Map, "u32FrmMaxQp"),
			U32FrmMinQp:                getUint(h264Map, "u32FrmMinQp"),
			U32FrmMinIQp:               getUint(h264Map, "u32FrmMinIQp"),
			U32FrmMaxIQp:               getUint(h264Map, "u32FrmMaxIQp"),
			U32MotionStaticSwitchFrmQp: getUint(h264Map, "u32MotionStaticSwitchFrmQp"),
		},
		H265: RcQpParams{
			S32FirstFrameStartQp:       getInt(h265Map, "s32FirstFrameStartQp"),
			U32StepQp:                  getUint(h265Map, "u32StepQp"),
			U32MinQp:                   getUint(h265Map, "u32MinQp"),
			U32MaxQp:                   getUint(h265Map, "u32MaxQp"),
			U32MinIQp:                  getUint(h265Map, "u32MinIQp"),
			U32MaxIQp:                  getUint(h265Map, "u32MaxIQp"),
			S32DeltIpQp:                getInt(h265Map, "s32DeltIpQp"),
			S32MaxReEncodeTimes:        getInt(h265Map, "s32MaxReEncodeTimes"),
			U32FrmMaxQp:                getUint(h265Map, "u32FrmMaxQp"),
			U32FrmMinQp:                getUint(h265Map, "u32FrmMinQp"),
			U32FrmMinIQp:               getUint(h265Map, "u32FrmMinIQp"),
			U32FrmMaxIQp:               getUint(h265Map, "u32FrmMaxIQp"),
			U32MotionStaticSwitchFrmQp: getUint(h265Map, "u32MotionStaticSwitchFrmQp"),
		},
	}
	return rc, nil
}

func rpcSetNpuAppStatus(enable bool) error {
	logger.Info().Bool("enable", enable).Msg("Setting NPU app status")
	var _, err = CallCtrlAction("set_yolo_enable", map[string]interface{}{"enable": enable})
	if err != nil {
		return err
	}

	config.NpuAppEnabled = enable
	if SaveConfig() != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcGetNpuAppStatus() (bool, error) {
	return config.NpuAppEnabled, nil
}

func rpcGetEDID() (string, error) {
	resp, err := CallCtrlAction("get_edid", nil)
	if err != nil {
		return "", err
	}
	edid, ok := resp.Result["edid"]
	if ok {
		return edid.(string), nil
	}
	return "", errors.New("EDID not found in response")
}

// defaultEdidHex is the EDID the app reports when nothing else is configured.
// It is a full 256-byte EDID: a 128-byte base block followed by a 128-byte
// CEA-861 extension block. Byte 126 (extension count) must stay consistent with
// the length, otherwise hosts may reject the EDID.
const defaultEdidHex = "00ffffffffffff0052620188008888881c150103800000780a0dc9a05747982712484c00000001010101010101010101010101010101023a801871382d40582c4500c48e2100001e011d007251d01e206e285500c48e2100001e000000fc00543734392d6648443732300a20000000fd00147801ff1d000a202020202020017b02031a714790040201112205230907018301000065030c0010008c0ad08a20e02d10103e9600138e2100001ed80980a020e02d101060a200c48e210000188c0ad090204031200c405500483900000018011d801871382d40582c4500c06c00000018011d8018711c1620582c2500c06c00000018000000000000000000000032"

// legacyTruncatedEdidHex is the base block of defaultEdidHex. Older builds used
// only this prefix as the default, which produced an EDID that announced one
// extension block without carrying it.
var legacyTruncatedEdidHex = defaultEdidHex[:256]

func rpcSetEDID(edid string) error {
	if edid == "" {
		logger.Info().Msg("Restoring EDID to default")
		edid = defaultEdidHex
	} else {
		logger.Info().Str("edid", edid).Msg("Setting EDID")
	}
	_, err := CallCtrlAction("set_edid", map[string]interface{}{"edid": edid})
	if err != nil {
		return err
	}

	// Save EDID to config, allowing it to be restored on reboot.
	config.EdidString = edid
	_ = SaveConfig()
	return nil
}

func rpcGetDefaultEDID() (string, error) {
	return defaultEdidHex, nil
}

func rpcSetForceHpd(forceHpd bool) error {
	forceHpdValue := 0
	if forceHpd {
		forceHpdValue = 1
	}

	forceHpdPath := "/sys/module/tc35874x/parameters/force_hpd"
	err := os.WriteFile(forceHpdPath, []byte(fmt.Sprintf("%d\n", forceHpdValue)), 0644)
	if err != nil {
		logger.Error().Err(err).Bool("force_hpd", forceHpd).Msg("Failed to set force_hpd parameter")
		return fmt.Errorf("failed to set force_hpd parameter: %w", err)
	}

	logger.Info().Bool("force_hpd", forceHpd).Msg("Force HPD setting applied")

	config.ForceHpd = forceHpd
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func rpcGetForceHpd() (bool, error) {
	forceHpdPath := "/sys/module/tc35874x/parameters/force_hpd"
	data, err := os.ReadFile(forceHpdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return config.ForceHpd, nil
		}
		logger.Error().Err(err).Msg("Failed to read force_hpd parameter")
		return config.ForceHpd, fmt.Errorf("failed to read force_hpd parameter: %w", err)
	}

	forceHpdValue := strings.TrimSpace(string(data))
	if forceHpdValue == "1" {
		return true, nil
	} else if forceHpdValue == "0" {
		return false, nil
	} else {
		logger.Warn().Str("force_hpd_value", forceHpdValue).Msg("Unexpected force_hpd value, using config value")
		return config.ForceHpd, nil
	}
}

func rpcGetLocalUpdateStatus() (*LocalMetadata, error) {
	var localStatus LocalMetadata
	systemVersionLocal, appVersionLocal, err := GetLocalVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get local version: %w", err)
	}
	localStatus.AppVersion = appVersionLocal.String()
	localStatus.SystemVersion = systemVersionLocal.String()
	return &localStatus, nil
}

func rpcGetUpdateStatus() (*UpdateStatus, error) {
	updateStatus, err := GetUpdateStatus(context.Background(), GetDeviceID())
	// to ensure backwards compatibility,
	// if there's an error, we won't return an error, but we will set the error field
	if err != nil {
		if updateStatus == nil {
			return nil, fmt.Errorf("error checking for updates: %w", err)
		}
		updateStatus.Error = err.Error()
	}

	return updateStatus, nil
}

type SelfSignatureStatus struct {
	AppSignatureAbsent  bool `json:"appSignatureAbsent,omitempty"`
	AppSignatureInvalid bool `json:"appSignatureInvalid,omitempty"`
	AppNoPublicKey      bool `json:"appNoPublicKey,omitempty"`
}

func rpcGetSelfSignatureStatus() (*SelfSignatureStatus, error) {
	return getSelfSignatureStatus(), nil
}

func getSelfSignatureStatus() *SelfSignatureStatus {
	status := &SelfSignatureStatus{}
	publicKey := getOTAPublicKey()

	appBinPath := "/userdata/picokvm/bin/kvm_app"
	appSigPath := appBinPath + ".sig"

	status.AppSignatureAbsent = isSigFileAbsent(appSigPath)

	if !status.AppSignatureAbsent {
		if publicKey == nil {
			status.AppNoPublicKey = true
		} else {
			status.AppSignatureInvalid = !verifyLocalFileSignature(appBinPath, appSigPath, publicKey)
		}
	}

	return status
}

func rpcTryUpdate() error {
	go func() {
		err := TryUpdate(context.Background(), GetDeviceID())
		if err != nil {
			logger.Warn().Err(err).Msg("failed to try update")
		}
	}()
	return nil
}

func rpcUpdateSignatures() (*SignatureUpdateResult, error) {
	result, err := UpdateSignatures(context.Background())
	if err != nil {
		logger.Warn().Err(err).Msg("failed to update signatures")
	}
	return result, err
}

func rpcGetCustomUpdateBaseURL() (string, error) {
	return customUpdateBaseURL, nil
}

func rpcSetCustomUpdateBaseURL(baseURL string) error {
	customUpdateBaseURL = baseURL
	return nil
}

func rpcGetUpdateDownloadProxy() (string, error) {
	return config.UpdateDownloadProxy, nil
}

func rpcSetUpdateDownloadProxy(proxy string) error {
	proxy = strings.TrimSpace(proxy)
	if proxy != "" {
		parsed, err := url.Parse(proxy)
		if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
			return fmt.Errorf("invalid update download proxy")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("update download proxy must use http or https")
		}
		if !strings.HasSuffix(proxy, "/") {
			proxy += "/"
		}
	}

	config.UpdateDownloadProxy = proxy
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcSetDisplayRotation(params DisplayRotationSettings) error {
	displaySettingsMu.Lock()
	defer displaySettingsMu.Unlock()

	var err error
	_, err = lvDispSetRotation(params.Rotation)
	if err == nil {
		config.DisplayRotation = params.Rotation
		if err := SaveConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}
	return err
}

func rpcGetDisplayRotation() (*DisplayRotationSettings, error) {
	return &DisplayRotationSettings{
		Rotation: config.DisplayRotation,
	}, nil
}

func rpcSetBacklightSettings(params BacklightSettings) error {
	blConfig := params

	// MaxBrightness is a percentage (10-100), the same unit the touchscreen slider uses.
	// Values below 10 would look like the panel is off, the dedicated display off
	// action is used for that instead.
	if blConfig.MaxBrightness > 100 || blConfig.MaxBrightness < 10 {
		return fmt.Errorf("maxBrightness must be between 10 and 100")
	}

	if blConfig.DimAfter < 0 {
		return fmt.Errorf("dimAfter must be a positive integer")
	}

	if blConfig.OffAfter < 0 {
		return fmt.Errorf("offAfter must be a positive integer")
	}

	config.DisplayBrightnessPercent = blConfig.MaxBrightness
	config.DisplayDimAfterSec = blConfig.DimAfter
	config.DisplayOffAfterSec = blConfig.OffAfter

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Keep the touchscreen backlight slider in sync with the web UI value.
	displaySettingsMu.Lock()
	pushBacklightToDisplay(config.DisplayBrightnessPercent)
	displaySettingsMu.Unlock()

	logger.Info().Int("max_brightness", config.DisplayBrightnessPercent).Int("dim_after", config.DisplayDimAfterSec).Int("off_after", config.DisplayOffAfterSec).Msg("rpc: display: settings applied")

	// If the device started up with auto-dim and/or auto-off set to zero, the display init
	// method will not have started the tickers. So in case that has changed, attempt to start the tickers now.
	startBacklightTickers()

	// Wake the display after the settings are altered, this ensures the tickers
	// are reset to the new settings, and will bring the display up to maxBrightness.
	// Calling with force set to true, to ignore the current state of the display, and force
	// it to reset the tickers.
	wakeDisplay(true)
	return nil
}

func rpcGetBacklightSettings() (*BacklightSettings, error) {
	return &BacklightSettings{
		MaxBrightness: config.DisplayBrightnessPercent,
		DimAfter:      int(config.DisplayDimAfterSec),
		OffAfter:      int(config.DisplayOffAfterSec),
	}, nil
}

// DisplayState is the payload returned by rpcToggleDisplay.
type DisplayState struct {
	On bool `json:"on"`
}

// rpcToggleDisplay switches the panel between dark and the configured
// brightness, backing the one click display off button in the web UI.
func rpcToggleDisplay() (*DisplayState, error) {
	on := toggleDisplayPower()

	return &DisplayState{On: on}, nil
}

// rpcGetDisplayState reports whether the panel is currently lit.
func rpcGetDisplayState() (*DisplayState, error) {
	return &DisplayState{On: backlightState != 2}, nil
}

func rpcSetTimeZone(timeZone string) error {
	var err error
	_, err = CallDisplayCtrlAction("set_timezone", map[string]interface{}{"timezone": timeZone})

	if err == nil {
		config.TimeZone = timeZone
		if err := SaveConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}

	return err
}

func rpcGetTimeZone() (string, error) {
	return config.TimeZone, nil
}

const (
	devModeFile = "/userdata/picokvm/devmode.enable"
	sshKeyDir   = "/userdata/openssh/.ssh"
	sshKeyFile  = "/userdata/openssh/.ssh/authorized_keys"
)

type DevModeState struct {
	Enabled bool `json:"enabled"`
}

type SSHKeyState struct {
	SSHKey string `json:"sshKey"`
}

func rpcGetDevModeState() (DevModeState, error) {
	devModeEnabled := false
	if _, err := os.Stat(devModeFile); err != nil {
		if !os.IsNotExist(err) {
			return DevModeState{}, fmt.Errorf("error checking dev mode file: %w", err)
		}
	} else {
		devModeEnabled = true
	}

	return DevModeState{
		Enabled: devModeEnabled,
	}, nil
}

func rpcSetDevModeState(enabled bool) error {
	if enabled {
		if err := os.WriteFile(devModeFile, []byte{}, 0644); err != nil {
			return fmt.Errorf("error enabling dev mode: %w", err)
		}
		return nil
	}

	if err := os.Remove(devModeFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error disabling dev mode: %w", err)
	}
	return nil
}

func rpcGetSSHKeyState() (string, error) {
	keyData, err := os.ReadFile(sshKeyFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("error reading SSH key file: %w", err)
		}
	}
	return string(keyData), nil
}

func rpcSetSSHKeyState(sshKey string) error {
	if sshKey != "" {
		// Create directory if it doesn't exist
		if err := os.MkdirAll(sshKeyDir, 0700); err != nil {
			return fmt.Errorf("failed to create SSH key directory: %w", err)
		}

		// Write SSH key to file
		if err := os.WriteFile(sshKeyFile, []byte(sshKey), 0600); err != nil {
			return fmt.Errorf("failed to write SSH key: %w", err)
		}
	} else {
		// Remove SSH key file if empty string is provided
		if err := os.Remove(sshKeyFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove SSH key file: %w", err)
		}
	}

	return nil
}

func rpcGetTLSState() TLSState {
	return getTLSState()
}

func rpcSetTLSState(state TLSState) error {
	err := setTLSState(state)
	if err != nil {
		return fmt.Errorf("failed to set TLS state: %w", err)
	}

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

type RPCHandler struct {
	Func   interface{}
	Params []string
}

// call the handler but recover from a panic to ensure our RPC thread doesn't collapse on malformed calls
func callRPCHandler(handler RPCHandler, params map[string]interface{}) (result interface{}, err error) {
	// Use defer to recover from a panic
	defer func() {
		if r := recover(); r != nil {
			// Convert the panic to an error
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("panic occurred: %v", r)
			}
		}
	}()

	// Call the handler
	result, err = riskyCallRPCHandler(handler, params)
	return result, err
}

func riskyCallRPCHandler(handler RPCHandler, params map[string]interface{}) (interface{}, error) {
	handlerValue := reflect.ValueOf(handler.Func)
	handlerType := handlerValue.Type()

	if handlerType.Kind() != reflect.Func {
		return nil, errors.New("handler is not a function")
	}

	numParams := handlerType.NumIn()
	args := make([]reflect.Value, numParams)
	// Get the parameter names from the RPCHandler
	paramNames := handler.Params

	if len(paramNames) != numParams {
		return nil, errors.New("mismatch between handler parameters and defined parameter names")
	}

	for i := 0; i < numParams; i++ {
		paramType := handlerType.In(i)
		paramName := paramNames[i]
		paramValue, ok := params[paramName]
		if !ok {
			return nil, errors.New("missing parameter: " + paramName)
		}

		convertedValue := reflect.ValueOf(paramValue)
		if !convertedValue.Type().ConvertibleTo(paramType) {
			if paramType.Kind() == reflect.Slice && (convertedValue.Kind() == reflect.Slice || convertedValue.Kind() == reflect.Array) {
				newSlice := reflect.MakeSlice(paramType, convertedValue.Len(), convertedValue.Len())
				for j := 0; j < convertedValue.Len(); j++ {
					elemValue := convertedValue.Index(j)
					if elemValue.Kind() == reflect.Interface {
						elemValue = elemValue.Elem()
					}
					if !elemValue.Type().ConvertibleTo(paramType.Elem()) {
						// Handle float64 to uint8 conversion
						if elemValue.Kind() == reflect.Float64 && paramType.Elem().Kind() == reflect.Uint8 {
							intValue := int(elemValue.Float())
							if intValue < 0 || intValue > 255 {
								return nil, fmt.Errorf("value out of range for uint8: %v", intValue)
							}
							newSlice.Index(j).SetUint(uint64(intValue))
						} else {
							fromType := elemValue.Type()
							toType := paramType.Elem()
							return nil, fmt.Errorf("invalid element type in slice for parameter %s: from %v to %v", paramName, fromType, toType)
						}
					} else {
						newSlice.Index(j).Set(elemValue.Convert(paramType.Elem()))
					}
				}
				args[i] = newSlice
			} else if paramType.Kind() == reflect.Struct && convertedValue.Kind() == reflect.Map {
				jsonData, err := json.Marshal(convertedValue.Interface())
				if err != nil {
					return nil, fmt.Errorf("failed to marshal map to JSON: %v", err)
				}

				newStruct := reflect.New(paramType).Interface()
				if err := json.Unmarshal(jsonData, newStruct); err != nil {
					return nil, fmt.Errorf("failed to unmarshal JSON into struct: %v", err)
				}
				args[i] = reflect.ValueOf(newStruct).Elem()
			} else {
				return nil, fmt.Errorf("invalid parameter type for: %s, type: %s", paramName, paramType.Kind())
			}
		} else {
			args[i] = convertedValue.Convert(paramType)
		}
	}

	results := handlerValue.Call(args)

	if len(results) == 0 {
		return nil, nil
	}

	if len(results) == 1 {
		if results[0].Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !results[0].IsNil() {
				return nil, results[0].Interface().(error)
			}
			return nil, nil
		}
		return results[0].Interface(), nil
	}

	if len(results) == 2 && results[1].Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		if !results[1].IsNil() {
			return nil, results[1].Interface().(error)
		}
		return results[0].Interface(), nil
	}

	return nil, errors.New("unexpected return values from handler")
}

func rpcSetMassStorageMode(mode string) (string, error) {
	logger.Info().Str("mode", mode).Msg("Setting mass storage mode")
	var cdrom bool
	switch mode {
	case "cdrom":
		cdrom = true
	case "file":
		cdrom = false
	default:
		logger.Info().Str("mode", mode).Msg("Invalid mode provided")
		return "", fmt.Errorf("invalid mode: %s", mode)
	}

	logger.Info().Str("mode", mode).Msg("Setting mass storage mode")

	err := setMassStorageMode(cdrom)
	if err != nil {
		return "", fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	logger.Info().Str("mode", mode).Msg("Mass storage mode set")

	// Get the updated mode after setting
	return rpcGetMassStorageMode()
}

func rpcGetMassStorageMode() (string, error) {
	cdrom, err := getMassStorageCDROMEnabled()
	if err != nil {
		return "", fmt.Errorf("failed to get mass storage mode: %w", err)
	}

	mode := "file"
	if cdrom {
		mode = "cdrom"
	}
	return mode, nil
}

func rpcIsUpdatePending() (bool, error) {
	return IsUpdatePending(), nil
}

func rpcGetUsbEmulationState() (bool, error) {
	return gadget.IsUDCBound()
}

func rpcSetUsbEmulationState(enabled bool) error {
	if !enabled {
		return gadget.UnbindUDCToDWC3()
	}

	// Recreate the gadget before the controller comes back. Devices went missing
	// after a plain rebind because the host enumerated a gadget whose functions
	// were only half alive; building a fresh instance first means the one
	// enumeration the host sees already carries every function.
	if err := rpcReinitializeUsbGadget(); err != nil {
		return fmt.Errorf("failed to reinitialize USB gadget before enabling emulation: %w", err)
	}

	// A full dwc3 unbind+bind rather than a bare bind: on the first enable after
	// boot the driver is already bound, and binding it again fails with
	// "no such device". The pair is idempotent.
	if err := gadget.RebindUsb(true); err != nil {
		return fmt.Errorf("failed to rebind USB controller: %w", err)
	}

	return nil
}

func rpcGetUsbEnhancedDetection() (bool, error) {
	ensureConfigLoaded()
	return config.UsbEnhancedDetection, nil
}

func rpcSetUsbEnhancedDetection(enabled bool) error {
	ensureConfigLoaded()
	if config.UsbEnhancedDetection == enabled {
		return nil
	}

	config.UsbEnhancedDetection = enabled
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if gadget != nil {
		checkUSBState()
	}

	return nil
}

func rpcGetUsbConfig() (usbgadget.Config, error) {
	LoadConfig()
	return *config.UsbConfig, nil
}

func rpcSetUsbConfig(usbConfig usbgadget.Config) error {
	LoadConfig()
	config.UsbConfig = &usbConfig
	gadget.SetGadgetConfig(config.UsbConfig)
	return updateUsbRelatedConfig()
}

func rpcGetWakeOnLanDevices() ([]WakeOnLanDevice, error) {
	if config.WakeOnLanDevices == nil {
		return []WakeOnLanDevice{}, nil
	}
	return config.WakeOnLanDevices, nil
}

type SetWakeOnLanDevicesParams struct {
	Devices []WakeOnLanDevice `json:"devices"`
}

func rpcSetWakeOnLanDevices(params SetWakeOnLanDevicesParams) error {
	config.WakeOnLanDevices = params.Devices
	return SaveConfig()
}

func rpcResetConfig() error {
	loadedConfig := *defaultConfig
	config = &loadedConfig
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to reset config: %w", err)
	}

	logger.Info().Msg("Configuration reset to default")
	return nil
}

func rpcGetConfigRaw() (string, error) {
	configLock.Lock()
	defer configLock.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	return string(data), nil
}

func rpcSetConfigRaw(configStr string) error {
	var newConfig Config
	if err := json.Unmarshal([]byte(configStr), &newConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	configLock.Lock()
	config = &newConfig
	configLock.Unlock()

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	logger.Info().Msg("Configuration updated via raw JSON")
	return nil
}

type RtcServersConfig struct {
	STUN        string       `json:"stun"`
	DefaultSTUN string       `json:"defaultStun"`
	TurnServers []TurnServer `json:"turnServers"`
}

func rpcGetRtcServersConfig() (RtcServersConfig, error) {
	return RtcServersConfig{
		STUN:        config.STUN,
		DefaultSTUN: DefaultSTUN,
		TurnServers: config.TurnServers,
	}, nil
}

func rpcSetStunServer(stun string) error {
	config.STUN = stun
	return SaveConfig()
}

type SetTurnServersParams struct {
	Servers []TurnServer `json:"servers"`
}

func rpcSetTurnServers(params SetTurnServersParams) error {
	config.TurnServers = params.Servers
	if config.TurnServers == nil {
		config.TurnServers = []TurnServer{}
	}
	return SaveConfig()
}

type IceServerJSON struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

func rpcGetIceServers() ([]IceServerJSON, error) {
	raw := buildICEServers()
	out := make([]IceServerJSON, 0, len(raw))
	for _, server := range raw {
		credential := ""
		if server.Credential != nil {
			credential = fmt.Sprintf("%v", server.Credential)
		}
		out = append(out, IceServerJSON{
			URLs:       server.URLs,
			Username:   server.Username,
			Credential: credential,
		})
	}
	return out, nil
}

func rpcGetActiveExtension() (string, error) {
	return config.ActiveExtension, nil
}

func rpcSetActiveExtension(extensionId string) error {
	if config.ActiveExtension == extensionId {
		return nil
	}
	config.ActiveExtension = extensionId
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

type SerialSettings struct {
	BaudRate  string `json:"baudRate"`
	DataBits  string `json:"dataBits"`
	StopBits  string `json:"stopBits"`
	Parity    string `json:"parity"`
	Connected bool   `json:"connected"`
}

func rpcGetSerialSettings() (SerialSettings, error) {
	settings := SerialSettings{
		BaudRate:  strconv.Itoa(serialPortMode.BaudRate),
		DataBits:  strconv.Itoa(serialPortMode.DataBits),
		StopBits:  "1",
		Parity:    "none",
		Connected: getSerialPort() != nil,
	}

	switch serialPortMode.StopBits {
	case serial.OneStopBit:
		settings.StopBits = "1"
	case serial.OnePointFiveStopBits:
		settings.StopBits = "1.5"
	case serial.TwoStopBits:
		settings.StopBits = "2"
	}

	switch serialPortMode.Parity {
	case serial.NoParity:
		settings.Parity = "none"
	case serial.OddParity:
		settings.Parity = "odd"
	case serial.EvenParity:
		settings.Parity = "even"
	case serial.MarkParity:
		settings.Parity = "mark"
	case serial.SpaceParity:
		settings.Parity = "space"
	}

	return settings, nil
}

var serialPortMode = defaultMode

// serialPortEnabled is the persisted user intent. When true the port is opened
// on boot and stays open across page reloads until the user disconnects it.
var serialPortEnabled bool

// loadSerialSettingsFromConfig loads serial port settings from config into serialPortMode.
// Called during LoadConfig to restore persisted settings.
func loadSerialSettingsFromConfig() {
	if config == nil {
		return
	}

	serialPortEnabled = config.SerialPortEnabled

	// Only apply if config has serial settings saved (non-zero baud rate)
	if config.SerialBaudRate == 0 {
		logger.Debug().Msg("no serial settings in config, using defaults")
		return
	}

	baudRate := config.SerialBaudRate
	dataBits := config.SerialDataBits
	if dataBits == 0 {
		dataBits = 8
	}

	var stopBits serial.StopBits
	switch config.SerialStopBits {
	case "1.5":
		stopBits = serial.OnePointFiveStopBits
	case "2":
		stopBits = serial.TwoStopBits
	default:
		stopBits = serial.OneStopBit
	}

	var parity serial.Parity
	switch config.SerialParity {
	case "odd":
		parity = serial.OddParity
	case "even":
		parity = serial.EvenParity
	case "mark":
		parity = serial.MarkParity
	case "space":
		parity = serial.SpaceParity
	default:
		parity = serial.NoParity
	}

	serialPortMode = &serial.Mode{
		BaudRate: baudRate,
		DataBits: dataBits,
		StopBits: stopBits,
		Parity:   parity,
	}

	logger.Info().
		Int("baud_rate", baudRate).
		Int("data_bits", dataBits).
		Str("stop_bits", config.SerialStopBits).
		Str("parity", config.SerialParity).
		Msg("serial settings loaded from config")
}

// saveSerialSettingsToConfig saves current serial port settings to config and persists to disk.
func saveSerialSettingsToConfig() error {
	if config == nil {
		return fmt.Errorf("config not loaded")
	}

	var stopBits string
	switch serialPortMode.StopBits {
	case serial.OnePointFiveStopBits:
		stopBits = "1.5"
	case serial.TwoStopBits:
		stopBits = "2"
	default:
		stopBits = "1"
	}

	var parity string
	switch serialPortMode.Parity {
	case serial.OddParity:
		parity = "odd"
	case serial.EvenParity:
		parity = "even"
	case serial.MarkParity:
		parity = "mark"
	case serial.SpaceParity:
		parity = "space"
	default:
		parity = "none"
	}

	config.SerialBaudRate = serialPortMode.BaudRate
	config.SerialDataBits = serialPortMode.DataBits
	config.SerialStopBits = stopBits
	config.SerialParity = parity
	config.SerialPortEnabled = serialPortEnabled

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save serial settings: %w", err)
	}

	logger.Info().
		Int("baud_rate", serialPortMode.BaudRate).
		Int("data_bits", serialPortMode.DataBits).
		Str("stop_bits", stopBits).
		Str("parity", parity).
		Msg("serial settings saved to config")

	return nil
}

func rpcSetSerialSettings(settings SerialSettings) error {
	logger.Info().
		Str("baud_rate", settings.BaudRate).
		Str("data_bits", settings.DataBits).
		Str("stop_bits", settings.StopBits).
		Str("parity", settings.Parity).
		Msg("rpcSetSerialSettings called")

	baudRate, err := strconv.Atoi(settings.BaudRate)
	if err != nil {
		logger.Error().Err(err).Msg("invalid baud rate")
		return fmt.Errorf("invalid baud rate: %v", err)
	}
	dataBits, err := strconv.Atoi(settings.DataBits)
	if err != nil {
		logger.Error().Err(err).Msg("invalid data bits")
		return fmt.Errorf("invalid data bits: %v", err)
	}

	var stopBits serial.StopBits
	switch settings.StopBits {
	case "1":
		stopBits = serial.OneStopBit
	case "1.5":
		stopBits = serial.OnePointFiveStopBits
	case "2":
		stopBits = serial.TwoStopBits
	default:
		logger.Error().Str("stop_bits", settings.StopBits).Msg("invalid stop bits")
		return fmt.Errorf("invalid stop bits: %s", settings.StopBits)
	}

	var parity serial.Parity
	switch settings.Parity {
	case "none":
		parity = serial.NoParity
	case "odd":
		parity = serial.OddParity
	case "even":
		parity = serial.EvenParity
	case "mark":
		parity = serial.MarkParity
	case "space":
		parity = serial.SpaceParity
	default:
		logger.Error().Str("parity", settings.Parity).Msg("invalid parity")
		return fmt.Errorf("invalid parity: %s", settings.Parity)
	}

	serialPortMode = &serial.Mode{
		BaudRate: baudRate,
		DataBits: dataBits,
		StopBits: stopBits,
		Parity:   parity,
	}

	logger.Info().
		Int("baud_rate", baudRate).
		Int("data_bits", dataBits).
		Interface("stop_bits", stopBits).
		Interface("parity", parity).
		Msg("serialPortMode updated")

	// Persist settings to config file
	if err := saveSerialSettingsToConfig(); err != nil {
		logger.Warn().Err(err).Msg("failed to persist serial settings")
		// Continue anyway - settings are still applied in memory
	}

	if p := getSerialPort(); p != nil {
		if err := p.SetMode(serialPortMode); err != nil {
			logger.Error().Err(err).Msg("failed to set port mode")
		} else {
			logger.Info().Msg("port mode updated successfully")
		}
	} else {
		logger.Warn().Msg("serial port is nil, mode will be applied on next open")
	}

	return nil
}

// rpcConnectSerial opens the serial port with current settings
func rpcConnectSerial() error {
	logger.Info().
		Int("baud_rate", serialPortMode.BaudRate).
		Int("data_bits", serialPortMode.DataBits).
		Msg("rpcConnectSerial called")

	// Only open when closed: a forced reopen would discard the bytes the kernel
	// has already buffered.
	if err := ensureSerialPortOpen(); err != nil {
		logger.Error().Err(err).Msg("failed to connect serial port")
		return fmt.Errorf("failed to connect serial port: %w", err)
	}

	// Persist the intent so page reloads and reboots keep the port open.
	serialPortEnabled = true
	if err := saveSerialSettingsToConfig(); err != nil {
		logger.Warn().Err(err).Msg("failed to persist serial port enabled state")
	}

	logger.Info().Msg("serial port connected successfully")
	return nil
}

// rpcDisconnectSerial closes the serial port
func rpcDisconnectSerial() error {
	logger.Info().Msg("rpcDisconnectSerial called")

	if getSerialPort() != nil {
		closeSerialPort()
		logger.Info().Msg("serial port disconnected")
	} else {
		logger.Warn().Msg("serial port was already disconnected")
	}

	// Clear the intent so the port is not reopened after a reload or a reboot.
	serialPortEnabled = false
	if err := saveSerialSettingsToConfig(); err != nil {
		logger.Warn().Err(err).Msg("failed to persist serial port enabled state")
	}

	return nil
}

func rpcGetUsbDevices() (usbgadget.Devices, error) {
	return *config.UsbDevices, nil
}

func updateUsbRelatedConfig() error {
	if err := gadget.UpdateGadgetConfig(); err != nil {
		return fmt.Errorf("failed to write gadget config: %w", err)
	}
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcSetUsbDevices(usbDevices usbgadget.Devices) error {
	mediaState, _ := rpcGetVirtualMediaState()
	if mediaState != nil && mediaState.Filename != "" {
		err := unmountImage(false)
		if err != nil {
			jsonRpcLogger.Error().Err(err).Msg("failed to unmount image")
		}
	}
	config.UsbDevices = &usbDevices
	gadget.SetGadgetDevices(config.UsbDevices)
	return updateUsbRelatedConfig()
}

func rpcSetUsbDeviceState(device string, enabled bool) error {
	switch device {
	case "absoluteMouse":
		config.UsbDevices.AbsoluteMouse = enabled
	case "relativeMouse":
		config.UsbDevices.RelativeMouse = enabled
	case "keyboard":
		config.UsbDevices.Keyboard = enabled
	case "massStorage":
		config.UsbDevices.MassStorage = enabled
	default:
		return fmt.Errorf("invalid device: %s", device)
	}
	gadget.SetGadgetDevices(config.UsbDevices)
	return updateUsbRelatedConfig()
}

func rpcGetKeyboardLayout() (string, error) {
	return config.KeyboardLayout, nil
}

func rpcSetKeyboardLayout(layout string) error {
	config.KeyboardLayout = layout
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

// keyboardFormFactors are the physical key arrangements the on-screen keyboard
// can mirror. They are separate from the language layout: the same language can
// be typed on any of them, and the form factor only decides which extra keys
// (IntlBackslash, yen/ro, IME keys) the keyboard shows.
var keyboardFormFactors = map[string]bool{"ANSI": true, "ISO": true, "JIS": true}

func rpcGetKeyboardFormFactor() (string, error) {
	// Configs written before this setting existed have an empty value; ANSI is
	// both the default and the shape those configs were rendered with.
	if !keyboardFormFactors[config.KeyboardFormFactor] {
		return "ANSI", nil
	}
	return config.KeyboardFormFactor, nil
}

func rpcSetKeyboardFormFactor(formFactor string) error {
	if !keyboardFormFactors[formFactor] {
		return fmt.Errorf("invalid keyboard form factor %q, expected ANSI, ISO or JIS", formFactor)
	}
	previous := config.KeyboardFormFactor
	config.KeyboardFormFactor = formFactor
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// JIS adds keys whose usages sit outside the range the default HID report
	// descriptor declares, so that arrangement needs a different descriptor. The
	// descriptor is a configfs attribute that is only read when the keyboard
	// function is bound, so the gadget has to be rebuilt and the host sees the
	// keyboard re-enumerate. ANSI and ISO share the default descriptor and skip this.
	if usbgadget.ApplyKeyboardReportDescForFormFactor(formFactor) {
		usbLogger.Info().Str("from", previous).Str("to", formFactor).Msg("form factor changes the HID report descriptor, reinitializing USB gadget")
		if err := rpcReinitializeUsbGadget(); err != nil {
			return fmt.Errorf("failed to apply the report descriptor for form factor %q: %w", formFactor, err)
		}
	}

	return nil
}

// keyboardClearStateKeys is the all-released key buffer used to blank the
// keyboard state.
var keyboardClearStateKeys = make([]byte, hidrpc.HidKeyBufferSize)

// isClearKeyStep reports whether a macro step releases every key.
func isClearKeyStep(step hidrpc.KeyboardMacroStep) bool {
	return step.Modifier == 0 && bytes.Equal(step.Keys, keyboardClearStateKeys)
}

var (
	keyboardMacroTask    *hidMacroTask
	keyboardMacroLock    sync.Mutex
	keyboardMacroRunLock sync.Mutex
)

const keyboardMacroOwner = "web:keyboard-macro"

// rpcDoExecuteKeyboardMacro replays macro steps on the gadget. Each step holds
// its keys down for Delay milliseconds. Cancellation is honoured between and
// during steps and always leaves the keyboard released, so a cancelled macro
// cannot strand a modifier on the remote host.
func rpcDoExecuteKeyboardMacro(ctx context.Context, macro []hidrpc.KeyboardMacroStep) error {
	jsonRpcLogger.Debug().Interface("steps", len(macro)).Msg("executing keyboard macro")
	// Runs while the macro owns the input lease and the execution mutex.
	defer func() {
		if err := rpcKeyboardReport(0, keyboardClearStateKeys); err != nil {
			jsonRpcLogger.Warn().Err(err).Msg("failed to reset keyboard state")
		}
	}()
	renew := time.NewTicker(5 * time.Second)
	defer renew.Stop()

	for i, step := range macro {
		if err := ctx.Err(); err != nil {
			return err
		}
		delay := time.Duration(step.Delay) * time.Millisecond

		release, err := mcp.AcquireInput(keyboardMacroOwner, mcp.DefaultInputHold)
		if err != nil {
			return err
		}
		release()
		if err := rpcKeyboardReport(step.Modifier, step.Keys); err != nil {
			jsonRpcLogger.Warn().Err(err).Msg("failed to execute keyboard macro")
			return err
		}

		// Notify the device that the keyboard state is being cleared so the
		// on-screen keyboard does not keep stale highlights.
		if isClearKeyStep(step) {
			if gadget != nil {
				gadget.UpdateKeysDown(0, keyboardClearStateKeys)
			}
		}

		timer := time.NewTimer(delay)
	wait:
		for {
			select {
			case <-timer.C:
				break wait
			case <-renew.C:
				// Long holds must not outlive the bounded input lease.
				release, err := mcp.AcquireInput(keyboardMacroOwner, mcp.DefaultInputHold)
				if err != nil {
					timer.Stop()
					return err
				}
				release()
			case <-ctx.Done():
				timer.Stop()
				jsonRpcLogger.Debug().Int("step", i).Msg("keyboard macro cancelled during sleep")
				return ctx.Err()
			}
		}
	}

	return nil
}

// rpcExecuteKeyboardMacro runs a macro received over HID RPC. The client is told
// when the macro starts and finishes so it can block concurrent typing.
func rpcExecuteKeyboardMacro(session *Session, task *hidMacroTask, report hidrpc.KeyboardMacroReport) error {
	if err := task.ctx.Err(); err != nil {
		return err
	}
	keyboardMacroLock.Lock()
	if keyboardMacroTask != nil {
		keyboardMacroTask.cancel()
	}
	keyboardMacroTask = task
	keyboardMacroLock.Unlock()

	// Do not let a replacement write until the previous runner has released keys.
	keyboardMacroRunLock.Lock()
	defer keyboardMacroRunLock.Unlock()
	defer func() {
		keyboardMacroLock.Lock()
		if keyboardMacroTask == task {
			keyboardMacroTask = nil
		}
		keyboardMacroLock.Unlock()
	}()
	if err := task.ctx.Err(); err != nil {
		return err
	}
	state := hidrpc.KeyboardMacroState{
		State:   true,
		IsPaste: report.IsPaste,
	}
	session.reportHidRPCKeyboardMacroState(state)
	defer func() {
		state.State = false
		session.reportHidRPCKeyboardMacroState(state)
	}()
	release, err := mcp.AcquireInput(keyboardMacroOwner, mcp.DefaultInputHold)
	if err != nil {
		return err
	}
	defer release()
	return rpcDoExecuteKeyboardMacro(task.ctx, report.Steps)
}

func getKeyboardMacros() (interface{}, error) {
	macros := make([]KeyboardMacro, len(config.KeyboardMacros))
	copy(macros, config.KeyboardMacros)

	return macros, nil
}

type KeyboardMacrosParams struct {
	Macros []interface{} `json:"macros"`
}

func setKeyboardMacros(params KeyboardMacrosParams) (interface{}, error) {
	if params.Macros == nil {
		return nil, fmt.Errorf("missing or invalid macros parameter")
	}

	newMacros := make([]KeyboardMacro, 0, len(params.Macros))

	for i, item := range params.Macros {
		macroMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid macro at index %d", i)
		}

		id, _ := macroMap["id"].(string)
		if id == "" {
			id = fmt.Sprintf("macro-%d", time.Now().UnixNano())
		}

		name, _ := macroMap["name"].(string)

		sortOrder := i + 1
		if sortOrderFloat, ok := macroMap["sortOrder"].(float64); ok {
			sortOrder = int(sortOrderFloat)
		}

		steps := []KeyboardMacroStep{}
		if stepsArray, ok := macroMap["steps"].([]interface{}); ok {
			for _, stepItem := range stepsArray {
				stepMap, ok := stepItem.(map[string]interface{})
				if !ok {
					continue
				}

				step := KeyboardMacroStep{}

				if keysArray, ok := stepMap["keys"].([]interface{}); ok {
					for _, k := range keysArray {
						if keyStr, ok := k.(string); ok {
							step.Keys = append(step.Keys, keyStr)
						}
					}
				}

				if modsArray, ok := stepMap["modifiers"].([]interface{}); ok {
					for _, m := range modsArray {
						if modStr, ok := m.(string); ok {
							step.Modifiers = append(step.Modifiers, modStr)
						}
					}
				}

				if delay, ok := stepMap["delay"].(float64); ok {
					step.Delay = int(delay)
				}

				steps = append(steps, step)
			}
		}

		macro := KeyboardMacro{
			ID:        id,
			Name:      name,
			Steps:     steps,
			SortOrder: sortOrder,
		}

		if err := macro.Validate(); err != nil {
			return nil, fmt.Errorf("invalid macro at index %d: %w", i, err)
		}

		newMacros = append(newMacros, macro)
	}

	config.KeyboardMacros = newMacros

	if err := SaveConfig(); err != nil {
		return nil, err
	}

	return nil, nil
}

func rpcGetLocalLoopbackOnly() (bool, error) {
	return config.LocalLoopbackOnly, nil
}

func rpcSetLocalLoopbackOnly(enabled bool) error {
	// Check if the setting is actually changing
	if config.LocalLoopbackOnly == enabled {
		return nil
	}

	// Update the setting
	config.LocalLoopbackOnly = enabled
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func rpcGetApiKey() (string, error) {
	return config.APIKey, nil
}

func rpcSetApiKey(apiKey string) error {
	config.APIKey = apiKey
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	// publish to the running MCP server, otherwise the new key only works after a restart
	mcp.ApplyAPIKey(apiKey)
	return nil
}

func rpcGenerateApiKey() (string, error) {
	key, err := generateAPIKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}
	config.APIKey = key
	if err := SaveConfig(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}
	mcp.ApplyAPIKey(key)
	return key, nil
}

type IOSettings struct {
	IO0Status bool `json:"io0Status"`
	IO1Status bool `json:"io1Status"`
}

func rpcGetIOSettings() (IOSettings, error) {
	LoadConfig()
	settings := IOSettings{
		IO0Status: config.IO0Status,
		IO1Status: config.IO1Status,
	}

	return settings, nil
}

func rpcSetIOSettings(settings IOSettings) error {
	LoadConfig()
	ioControlMu.Lock()
	defer ioControlMu.Unlock()
	// IO0: GPIO58 IO1: GPIO59
	_ = setGPIOValue(58, settings.IO0Status)
	_ = setGPIOValue(59, settings.IO1Status)

	config.IO0Status = settings.IO0Status
	config.IO1Status = settings.IO1Status
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func rpcSetIOStatus(ioName string, status bool) error {
	var pin int
	if ioName == "power" {
		pin = 58
	} else if ioName == "reset" {
		pin = 59
	} else {
		return fmt.Errorf("unknown IO name: %s", ioName)
	}

	ioControlMu.Lock()
	defer ioControlMu.Unlock()

	if err := setGPIOValue(pin, status); err != nil {
		return fmt.Errorf("failed to set GPIO value: %v", err)
	}
	return nil
}

func rpcTriggerPower() error {
	go func() {
		if err := pulseGPIO(58, 2*time.Second); err != nil {
			logger.Error().Err(err).Msg("Failed to trigger power pulse")
		}
	}()
	return nil
}

func rpcTriggerReset() error {
	go func() {
		if err := pulseGPIO(59, 2*time.Second); err != nil {
			logger.Error().Err(err).Msg("Failed to trigger reset pulse")
		}
	}()
	return nil
}

func rpcResetIOInput() error {
	if err := resetIOInput(); err != nil {
		return err
	}
	return nil
}

func rpcGetIOInputStatus() (map[string]bool, error) {
	// IO2: GPIO0 - Power LED
	// IO3: GPIO1 - HDD LED
	powerLed, err := getGPIOValue(0)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to read Power LED status")
		// Don't return error, just default to false
	}

	hddLed, err := getGPIOValue(1)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to read HDD LED status")
		// Don't return error, just default to false
	}

	// Active Low: Low level means LED is ON (Radio active)
	return map[string]bool{
		"powerLed": !powerLed,
		"hddLed":   !hddLed,
	}, nil
}

func rpcGetAudioMode() (string, error) {
	return config.AudioMode, nil
}

func rpcSetAudioMode(mode string) error {
	config.AudioMode = mode
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if config.AudioMode != "disabled" {
		StartNtpAudioServer(handleAudioClient)
	} else {
		StopNtpAudioServer()
	}

	return nil
}

func rpcSetLedGreenMode(mode string) error {
	err := setLedMode(ledGreenPath, mode)
	if err != nil {
		return err
	}

	config.LEDGreenMode = mode
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcSetLedYellowMode(mode string) error {
	err := setLedMode(ledYellowPath, mode)
	if err != nil {
		return err
	}

	config.LEDYellowMode = mode
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcGetLedGreenMode() (string, error) {
	return config.LEDGreenMode, nil
}

func rpcGetLedYellowMode() (string, error) {
	return config.LEDYellowMode, nil
}
func rpcGetAutoMountImage() (*AutoMountImageConfig, error) {
	return config.AutoMountImage, nil
}

func rpcSetAutoMountImage(filename string, source string) error {
	if filename == "" {
		// Disabling auto-mount must also drop the persisted virtual media entry for
		// the same image, otherwise remountPersistedVirtualMediaState brings it back
		// on the next boot.
		clearPersistedAutoMountState(config.AutoMountImage, config.PersistedVirtualMediaState)
		config.AutoMountImage = nil
	} else {
		if source != "kvm" && source != "sd" {
			return fmt.Errorf("invalid source: %s, must be 'kvm' or 'sd'", source)
		}
		config.AutoMountImage = &AutoMountImageConfig{
			Filename: filename,
			Source:   source,
		}
	}
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func rpcGetFirewallConfig() (FirewallConfig, error) {
	LoadConfig()
	if systemCfg, err := ReadFirewallConfigFromSystem(); err == nil && systemCfg != nil {
		return *systemCfg, nil
	}
	if config.Firewall == nil {
		return *defaultConfig.Firewall, nil
	}
	return *config.Firewall, nil
}

func rpcSetFirewallConfig(firewallCfg FirewallConfig) error {
	LoadConfig()
	managedCfg := firewallCfg
	managedCfg.PortForwards = filterManagedPortForwards(firewallCfg.PortForwards)
	if err := ApplyFirewallConfig(&managedCfg); err != nil {
		return err
	}
	config.Firewall = &managedCfg
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

func filterManagedPortForwards(in []FirewallPortRule) []FirewallPortRule {
	out := make([]FirewallPortRule, 0, len(in))
	for _, r := range in {
		if r.Managed != nil && !*r.Managed {
			continue
		}
		out = append(out, r)
	}
	return out
}

func rpcConfirmOtherSession() (bool, error) {
	return true, nil
}

// StartJpegCapture starts continuous JPEG capture mode.
func StartJpegCapture() error {
	_, err := CallCtrlAction("jpeg_capture_start", nil)
	if err != nil {
		return fmt.Errorf("failed to start JPEG capture: %w", err)
	}
	return nil
}

// StopJpegCapture stops continuous JPEG capture mode.
func StopJpegCapture() error {
	_, err := CallCtrlAction("jpeg_capture_stop", nil)
	if err != nil {
		return fmt.Errorf("failed to stop JPEG capture: %w", err)
	}
	return nil
}

const (
	snapshotReadyTimeout = 5 * time.Second
	snapshotAttempts     = 2
	snapshotPollAttempts = 20
	snapshotPollInterval = 250 * time.Millisecond
	snapshotStreamLinger = 15 * time.Second
)

var (
	snapshotStreamMu    sync.Mutex
	snapshotStreamSubID string
	snapshotStreamTimer *time.Timer
)

// ensureSnapshotStream makes sure the native encoder is running. It is only started
// while a video consumer is attached (main.go onFirstSubscribe/onLastUnsubscribe,
// webrtc.go onFirstSessionConnected), so with no WebUI open it stays idle and the
// native app answers "snapshot failed". Subscribing starts it, and the first frame
// confirms the encoder is actually producing data.
func ensureSnapshotStream() error {
	snapshotStreamMu.Lock()
	defer snapshotStreamMu.Unlock()

	if snapshotStreamSubID != "" {
		// a previous capture already started the stream, just extend its linger
		if snapshotStreamTimer != nil {
			snapshotStreamTimer.Reset(snapshotStreamLinger)
		}
		return nil
	}

	subID, frames := videoBroadcaster.Subscribe()
	snapshotStreamSubID = subID

	// Drain the subscription for its whole lifetime. Every subscriber gets a 1MB
	// pooled buffer per frame (maxFrameSize = 1920*1080/2) and the channel holds up
	// to 200 of them, so a subscriber that stops reading pins ~200MB and the device
	// runs out of memory. Draining keeps the channel empty and returns buffers to the
	// pool.
	firstFrame := make(chan struct{}, 1)
	go func() {
		for frame := range frames {
			frame.Release()
			select {
			case firstFrame <- struct{}{}:
			default:
			}
		}
	}()

	select {
	case <-firstFrame:
		return nil
	case <-time.After(snapshotReadyTimeout):
		return fmt.Errorf("no video frame received within %s", snapshotReadyTimeout)
	}
}

// releaseSnapshotStream keeps the stream alive for a linger period so back-to-back
// captures do not restart the native encoder each time.
func releaseSnapshotStream() {
	snapshotStreamMu.Lock()
	defer snapshotStreamMu.Unlock()

	if snapshotStreamSubID == "" {
		return
	}

	if snapshotStreamTimer == nil {
		snapshotStreamTimer = time.AfterFunc(snapshotStreamLinger, stopSnapshotStream)
		return
	}

	snapshotStreamTimer.Reset(snapshotStreamLinger)
}

// stopSnapshotStream drops the snapshot subscription; the broadcaster then stops the
// native stream unless somebody else is still watching.
func stopSnapshotStream() {
	snapshotStreamMu.Lock()
	subID := snapshotStreamSubID
	snapshotStreamSubID = ""
	snapshotStreamMu.Unlock()

	if subID != "" {
		videoBroadcaster.Unsubscribe(subID)
	}
}

func readSnapshotFile() ([]byte, error) {
	data, err := os.ReadFile(snapshotSpoolPath)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("snapshot file is empty")
	}
	return data, nil
}

// takeSnapshot triggers one JPEG capture and reads the encoded file.
func takeSnapshot() ([]byte, error) {
	os.Remove(snapshotSpoolPath)

	// drain any stale signal before triggering
	select {
	case <-jpegReadyCh:
	default:
	}

	if _, err := CallCtrlAction("jpeg_take_snapshot", nil); err != nil {
		logger.Error().Err(err).Msg("jpeg_take_snapshot failed")
		return nil, fmt.Errorf("failed to trigger JPEG capture: %w", err)
	}

	// wait for the jpeg_ready event from native, fall back to polling on timeout
	select {
	case <-jpegReadyCh:
		if data, err := readSnapshotFile(); err == nil {
			logger.Info().Int("size", len(data)).Msg("JPEG captured successfully")
			return data, nil
		} else {
			logger.Warn().Err(err).Msg("jpeg_ready received but snapshot file not readable")
		}
	case <-time.After(snapshotReadyTimeout):
		logger.Warn().Msg("jpeg_ready event not received, falling back to polling")
	}

	for i := 0; i < snapshotPollAttempts; i++ {
		if data, err := readSnapshotFile(); err == nil {
			logger.Info().Int("size", len(data)).Msg("JPEG captured (fallback polling)")
			return data, nil
		}
		time.Sleep(snapshotPollInterval)
	}

	return nil, fmt.Errorf("JPEG file not found at %s", snapshotSpoolPath)
}

// captureScreenshot captures a JPEG screenshot using hardware encoder.
// It is serialized: the native encoder owns a single spool file.
func captureScreenshot(format string) ([]byte, error) {
	if format != "jpeg" && format != "jpg" {
		return nil, fmt.Errorf("only JPEG format is supported")
	}

	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	logger.Info().Msg("triggering JPEG snapshot via jpeg_take_snapshot")

	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		logger.Warn().Err(err).Msg("failed to create screenshot directory")
	}

	if err := ensureSnapshotStream(); err != nil {
		logger.Warn().Err(err).Msg("snapshot stream not ready, trying snapshot anyway")
	}
	defer releaseSnapshotStream()

	var lastErr error
	for attempt := 1; attempt <= snapshotAttempts; attempt++ {
		data, err := takeSnapshot()
		if err == nil {
			return data, nil
		}
		lastErr = err
		logger.Warn().Err(err).Int("attempt", attempt).Msg("snapshot attempt failed")
		if attempt < snapshotAttempts {
			time.Sleep(500 * time.Millisecond)
		}
	}

	return nil, lastErr
}

var rpcHandlers = map[string]RPCHandler{
	"ping":                     {Func: rpcPing},
	"reboot":                   {Func: rpcReboot, Params: []string{"force"}},
	"getDeviceID":              {Func: rpcGetDeviceID},
	"getNetworkState":          {Func: rpcGetNetworkState},
	"getNetworkSettings":       {Func: rpcGetNetworkSettings},
	"setNetworkSettings":       {Func: rpcSetNetworkSettings, Params: []string{"settings"}},
	"getTimeSyncServers":       {Func: rpcGetTimeSyncServers},
	"setEthernetMacAddress":    {Func: rpcSetEthernetMacAddress, Params: []string{"macAddress"}},
	"renewDHCPLease":           {Func: rpcRenewDHCPLease},
	"requestDHCPAddress":       {Func: rpcRequestDHCPAddress, Params: []string{"ip"}},
	"keyboardReport":           {Func: rpcKeyboardReport, Params: []string{"modifier", "keys"}},
	"keypressReport":           {Func: rpcKeypressReport, Params: []string{"key", "press"}},
	"keypressKeepAlive":        {Func: rpcKeypressKeepAlive},
	"getKeyboardLedState":      {Func: rpcGetKeyboardLedState},
	"absMouseReport":           {Func: rpcAbsMouseReport, Params: []string{"x", "y", "buttons"}},
	"relMouseReport":           {Func: rpcRelMouseReport, Params: []string{"dx", "dy", "buttons"}},
	"wheelReport":              {Func: rpcWheelReport, Params: []string{"wheelY", "wheelX"}},
	"getVideoState":            {Func: rpcGetVideoState},
	"getUSBState":              {Func: rpcGetUSBState},
	"reinitializeUsbGadget":    {Func: rpcReinitializeUsbGadgetManually},
	"unmountImage":             {Func: rpcUnmountImage},
	"rpcMountBuiltInImage":     {Func: rpcMountBuiltInImage, Params: []string{"filename"}},
	"setJigglerState":          {Func: rpcSetJigglerState, Params: []string{"enabled"}},
	"getJigglerState":          {Func: rpcGetJigglerState},
	"sendUsbWakeupSignal":      {Func: rpcSendUsbWakeupSignal},
	"sendWOLMagicPacket":       {Func: rpcSendWOLMagicPacket, Params: []string{"macAddress"}},
	"getStreamQualityFactor":   {Func: rpcGetStreamQualityFactor},
	"setStreamQualityFactor":   {Func: rpcSetStreamQualityFactor, Params: []string{"factor"}},
	"getEDID":                  {Func: rpcGetEDID},
	"getDefaultEDID":           {Func: rpcGetDefaultEDID},
	"setEDID":                  {Func: rpcSetEDID, Params: []string{"edid"}},
	"setForceHpd":              {Func: rpcSetForceHpd, Params: []string{"forceHpd"}},
	"getForceHpd":              {Func: rpcGetForceHpd},
	"getLocalUpdateStatus":     {Func: rpcGetLocalUpdateStatus},
	"getUpdateStatus":          {Func: rpcGetUpdateStatus},
	"getSelfSignatureStatus":   {Func: rpcGetSelfSignatureStatus},
	"tryUpdate":                {Func: rpcTryUpdate},
	"updateSignatures":         {Func: rpcUpdateSignatures},
	"getCustomUpdateBaseURL":   {Func: rpcGetCustomUpdateBaseURL},
	"setCustomUpdateBaseURL":   {Func: rpcSetCustomUpdateBaseURL, Params: []string{"baseURL"}},
	"getUpdateDownloadProxy":   {Func: rpcGetUpdateDownloadProxy},
	"setUpdateDownloadProxy":   {Func: rpcSetUpdateDownloadProxy, Params: []string{"proxy"}},
	"getDevModeState":          {Func: rpcGetDevModeState},
	"setDevModeState":          {Func: rpcSetDevModeState, Params: []string{"enabled"}},
	"getSSHKeyState":           {Func: rpcGetSSHKeyState},
	"setSSHKeyState":           {Func: rpcSetSSHKeyState, Params: []string{"sshKey"}},
	"getApiKey":                {Func: rpcGetApiKey},
	"setApiKey":                {Func: rpcSetApiKey, Params: []string{"apiKey"}},
	"generateApiKey":           {Func: rpcGenerateApiKey},
	"getTLSState":              {Func: rpcGetTLSState},
	"setTLSState":              {Func: rpcSetTLSState, Params: []string{"state"}},
	"setMassStorageMode":       {Func: rpcSetMassStorageMode, Params: []string{"mode"}},
	"getMassStorageMode":       {Func: rpcGetMassStorageMode},
	"isUpdatePending":          {Func: rpcIsUpdatePending},
	"getUsbEmulationState":     {Func: rpcGetUsbEmulationState},
	"setUsbEmulationState":     {Func: rpcSetUsbEmulationState, Params: []string{"enabled"}},
	"getUsbEnhancedDetection":  {Func: rpcGetUsbEnhancedDetection},
	"setUsbEnhancedDetection":  {Func: rpcSetUsbEnhancedDetection, Params: []string{"enabled"}},
	"getUsbConfig":             {Func: rpcGetUsbConfig},
	"setUsbConfig":             {Func: rpcSetUsbConfig, Params: []string{"usbConfig"}},
	"checkMountUrl":            {Func: rpcCheckMountUrl, Params: []string{"url"}},
	"getVirtualMediaState":     {Func: rpcGetVirtualMediaState},
	"getStorageSpace":          {Func: rpcGetStorageSpace},
	"getSDStorageSpace":        {Func: rpcGetSDStorageSpace},
	"resetSDStorage":           {Func: rpcResetSDStorage},
	"mountSDStorage":           {Func: rpcMountSDStorage},
	"unmountSDStorage":         {Func: rpcUnmountSDStorage},
	"formatSDStorage":          {Func: rpcFormatSDStorage, Params: []string{"confirm", "fsType"}},
	"mountWithHTTP":            {Func: rpcMountWithHTTP, Params: []string{"url", "mode"}},
	"mountWithWebRTC":          {Func: rpcMountWithWebRTC, Params: []string{"filename", "size", "mode"}},
	"mountWithStorage":         {Func: rpcMountWithStorage, Params: []string{"filename", "mode"}},
	"mountWithSDStorage":       {Func: rpcMountWithSDStorage, Params: []string{"filename", "mode"}},
	"setAutoMountImage":        {Func: rpcSetAutoMountImage, Params: []string{"filename", "source"}},
	"getAutoMountImage":        {Func: rpcGetAutoMountImage},
	"confirmOtherSession":      {Func: rpcConfirmOtherSession},
	"listStorageFiles":         {Func: rpcListStorageFiles},
	"deleteStorageFile":        {Func: rpcDeleteStorageFile, Params: []string{"filename"}},
	"startStorageFileUpload":   {Func: rpcStartStorageFileUpload, Params: []string{"filename", "size"}},
	"listSDStorageFiles":       {Func: rpcListSDStorageFiles},
	"deleteSDStorageFile":      {Func: rpcDeleteSDStorageFile, Params: []string{"filename"}},
	"startSDStorageFileUpload": {Func: rpcStartSDStorageFileUpload, Params: []string{"filename", "size"}},
	"getWakeOnLanDevices":      {Func: rpcGetWakeOnLanDevices},
	"setWakeOnLanDevices":      {Func: rpcSetWakeOnLanDevices, Params: []string{"params"}},
	"resetConfig":              {Func: rpcResetConfig},
	"getConfigRaw":             {Func: rpcGetConfigRaw},
	"setConfigRaw":             {Func: rpcSetConfigRaw, Params: []string{"configStr"}},
	"getRtcServersConfig":      {Func: rpcGetRtcServersConfig},
	"setStunServer":            {Func: rpcSetStunServer, Params: []string{"stun"}},
	"setTurnServers":           {Func: rpcSetTurnServers, Params: []string{"params"}},
	"getIceServers":            {Func: rpcGetIceServers},
	"setDisplayRotation":       {Func: rpcSetDisplayRotation, Params: []string{"params"}},
	"getDisplayRotation":       {Func: rpcGetDisplayRotation},
	"setBacklightSettings":     {Func: rpcSetBacklightSettings, Params: []string{"params"}},
	"getBacklightSettings":     {Func: rpcGetBacklightSettings},
	"toggleDisplay":            {Func: rpcToggleDisplay},
	"getDisplayState":          {Func: rpcGetDisplayState},
	"setTimeZone":              {Func: rpcSetTimeZone, Params: []string{"timeZone"}},
	"getTimeZone":              {Func: rpcGetTimeZone},
	"setLedGreenMode":          {Func: rpcSetLedGreenMode, Params: []string{"mode"}},
	"setLedYellowMode":         {Func: rpcSetLedYellowMode, Params: []string{"mode"}},
	"getLedGreenMode":          {Func: rpcGetLedGreenMode},
	"getLedYellowMode":         {Func: rpcGetLedYellowMode},
	"getActiveExtension":       {Func: rpcGetActiveExtension},
	"setActiveExtension":       {Func: rpcSetActiveExtension, Params: []string{"extensionId"}},
	"getSerialSettings":        {Func: rpcGetSerialSettings},
	"setSerialSettings":        {Func: rpcSetSerialSettings, Params: []string{"settings"}},
	"connectSerial":            {Func: rpcConnectSerial},
	"disconnectSerial":         {Func: rpcDisconnectSerial},
	"getUsbDevices":            {Func: rpcGetUsbDevices},
	"setUsbDevices":            {Func: rpcSetUsbDevices, Params: []string{"devices"}},
	"setUsbDeviceState":        {Func: rpcSetUsbDeviceState, Params: []string{"device", "enabled"}},
	"getKeyboardLayout":        {Func: rpcGetKeyboardLayout},
	"setKeyboardLayout":        {Func: rpcSetKeyboardLayout, Params: []string{"layout"}},
	"getKeyboardFormFactor":    {Func: rpcGetKeyboardFormFactor},
	"setKeyboardFormFactor":    {Func: rpcSetKeyboardFormFactor, Params: []string{"formFactor"}},
	"getKeyboardMacros":        {Func: getKeyboardMacros},
	"setKeyboardMacros":        {Func: setKeyboardMacros, Params: []string{"params"}},
	"getLocalLoopbackOnly":     {Func: rpcGetLocalLoopbackOnly},
	"setLocalLoopbackOnly":     {Func: rpcSetLocalLoopbackOnly, Params: []string{"enabled"}},
	"getIOSettings":            {Func: rpcGetIOSettings},
	"setIOSettings":            {Func: rpcSetIOSettings, Params: []string{"settings"}},
	"triggerPower":             {Func: rpcTriggerPower},
	"triggerReset":             {Func: rpcTriggerReset},
	"setIOStatus":              {Func: rpcSetIOStatus, Params: []string{"ioName", "status"}},
	"getIOInputStatus":         {Func: rpcGetIOInputStatus},
	"resetIOInput":             {Func: rpcResetIOInput},
	"getSDMountStatus":         {Func: rpcGetSDMountStatus},
	"loginTailScale":           {Func: rpcLoginTailScale, Params: []string{"xEdge"}},
	"logoutTailScale":          {Func: rpcLogoutTailScale},
	"cancelTailScale":          {Func: rpcCancelTailScale},
	"getTailScaleSettings":     {Func: rpcGetTailScaleSettings},
	"loginZeroTier":            {Func: rpcLoginZeroTier, Params: []string{"networkID"}},
	"logoutZeroTier":           {Func: rpcLogoutZeroTier, Params: []string{"networkID"}},
	"getZeroTierSettings":      {Func: rpcGetZeroTierSettings},
	"setUpdateSource":          {Func: rpcSetUpdateSource, Params: []string{"source"}},
	"getAudioMode":             {Func: rpcGetAudioMode},
	"setAudioMode":             {Func: rpcSetAudioMode, Params: []string{"mode"}},
	"startFrpc":                {Func: rpcStartFrpc, Params: []string{"frpcToml"}},
	"stopFrpc":                 {Func: rpcStopFrpc},
	"getFrpcStatus":            {Func: rpcGetFrpcStatus},
	"getFrpcToml":              {Func: rpcGetFrpcToml},
	"getFrpcLog":               {Func: rpcGetFrpcLog},
	"startEasyTier":            {Func: rpcStartEasyTier, Params: []string{"name", "secret", "node"}},
	"stopEasyTier":             {Func: rpcStopEasyTier},
	"getEasyTierStatus":        {Func: rpcGetEasyTierStatus},
	"getEasyTierConfig":        {Func: rpcGetEasyTierConfig},
	"getEasyTierLog":           {Func: rpcGetEasyTierLog},
	"startVnt":                 {Func: rpcStartVnt, Params: []string{"config_mode", "token", "device_id", "name", "server_addr", "config_file", "model", "password"}},
	"stopVnt":                  {Func: rpcStopVnt},
	"getVntStatus":             {Func: rpcGetVntStatus},
	"getVntConfig":             {Func: rpcGetVntConfig},
	"getVntConfigFile":         {Func: rpcGetVntConfigFile},
	"getVntLog":                {Func: rpcGetVntLog},
	"getVntInfo":               {Func: rpcGetVntInfo},
	"getEasyTierNodeInfo":      {Func: rpcGetEasyTierNodeInfo},
	"startCloudflared":         {Func: rpcStartCloudflared, Params: []string{"token"}},
	"stopCloudflared":          {Func: rpcStopCloudflared},
	"getCloudflaredStatus":     {Func: rpcGetCloudflaredStatus},
	"getCloudflaredLog":        {Func: rpcGetCloudflaredLog},
	"getVpnToolSystemInfo":     {Func: rpcGetVpnToolSystemInfo},
	"getVpnToolStatus":         {Func: rpcGetVpnToolStatus, Params: []string{"tool"}},
	"listVpnToolReleases":      {Func: rpcListVpnToolReleases, Params: []string{"tool"}},
	"installVpnTool":           {Func: rpcInstallVpnTool, Params: []string{"tool", "version", "assetName", "downloadURL"}},
	"startVpnToolInstall":      {Func: rpcStartVpnToolInstall, Params: []string{"tool", "version", "assetName", "downloadURL"}},
	"getVpnToolInstallTask":    {Func: rpcGetVpnToolInstallTask, Params: []string{"tool"}},
	"useVpnToolVersion":        {Func: rpcUseVpnToolVersion, Params: []string{"tool", "version"}},
	"uninstallVpnToolVersion":  {Func: rpcUninstallVpnToolVersion, Params: []string{"tool", "version"}},
	"getStreamEncodecType":     {Func: rpcGetStreamEncodecType},
	"setStreamEncodecType":     {Func: rpcSetStreamEncodecType, Params: []string{"encodecType"}},
	"setVideoRc":               {Func: rpcSetVideoRc, Params: []string{"params"}},
	"getVideoRc":               {Func: rpcGetVideoRc},
	"setNpuAppStatus":          {Func: rpcSetNpuAppStatus, Params: []string{"enable"}},
	"getNpuAppStatus":          {Func: rpcGetNpuAppStatus},
	"startWireguard":           {Func: rpcStartWireguard, Params: []string{"configFile"}},
	"stopWireguard":            {Func: rpcStopWireguard},
	"getWireguardStatus":       {Func: rpcGetWireguardStatus},
	"getWireguardConfig":       {Func: rpcGetWireguardConfig},
	"getWireguardLog":          {Func: rpcGetWireguardLog},
	"getWireguardInfo":         {Func: rpcGetWireguardInfo},
	"getFirewallConfig":        {Func: rpcGetFirewallConfig},
	"setFirewallConfig":        {Func: rpcSetFirewallConfig, Params: []string{"config"}},
	"getBootStorageType":       {Func: rpcGetBootStorageType},
	"getLocalPackageInfo":      {Func: rpcGetLocalPackageInfo},
	"clearLocalPackage":        {Func: rpcClearLocalPackage},
	"getNetbirdStatus":         {Func: rpcGetNetbirdStatus},
	"startNetbird":             {Func: rpcStartNetbird},
	"stopNetbird":              {Func: rpcStopNetbird},
	"netbirdUp":                {Func: rpcNetbirdUp, Params: []string{"managementUrl"}},
	"netbirdDown":              {Func: rpcNetbirdDown},
	"getNetbirdLog":            {Func: rpcGetNetbirdLog},
	"getNetbirdVersion":        {Func: rpcGetNetbirdVersion},
	"getNetbirdUpLog":          {Func: rpcGetNetbirdUpLog},
	"getNetbirdStatusText":     {Func: rpcGetNetbirdStatusText},
	"getVpnAutoStartStatus":    {Func: rpcGetVpnAutoStartStatus},
}
