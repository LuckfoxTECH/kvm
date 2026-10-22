package kvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// displayActions are the touchscreen requests sent by kvm_display over the
// display control socket.
const (
	displayActionPowerPress    = "power_press"
	displayActionPowerRelease  = "power_release"
	displayActionResetPress    = "reset_press"
	displayActionResetRelease  = "reset_release"
	displayActionRotationCycle = "rotation_cycle"
	displayActionDisplayOff    = "display_off"
	displayActionBacklight     = "backlight"
	displayActionFactoryReset  = "factory_reset"
	displayActionNetworkReset  = "network_reset"
	displayActionUsbReinit     = "usb_reinit"
	// displayActionRotationChanged is reported after the display rotated itself.
	displayActionRotationChanged = "rotation_changed"
)

// backlightFullScale is the documented hardware maximum of the backlight class.
// The touchscreen slider is expressed in percent of that range.
const backlightFullScale = 200

// ioHoldSafetyTimeout releases a power/reset line that was left high, e.g. when
// the touchscreen stopped sending the release event.
const ioHoldSafetyTimeout = 10 * time.Second

type displayActionPayload struct {
	Action string `json:"action"`
	Value  int    `json:"value"`
}

// displaySettingsMu serialises backlight, rotation and power/reset changes that
// come from the touchscreen and from the web UI, so both happen to know the
// current state and cannot overwrite each other.
var displaySettingsMu sync.Mutex

var ioHoldTimers = struct {
	sync.Mutex
	timers map[string]*time.Timer
}{timers: map[string]*time.Timer{}}

// HandleDisplayActionMessage dispatches an action requested from the touchscreen.
func HandleDisplayActionMessage(resp CtrlResponse) {
	payload := displayActionPayload{}
	if len(resp.Data) == 0 {
		displayLogger.Warn().Msg("display action without payload")
		return
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		displayLogger.Warn().Err(err).Str("data", string(resp.Data)).Msg("failed to parse display action")
		return
	}

	switch payload.Action {
	case displayActionPowerPress:
		holdIO("power", true)
	case displayActionPowerRelease:
		holdIO("power", false)
	case displayActionResetPress:
		holdIO("reset", true)
	case displayActionResetRelease:
		holdIO("reset", false)
	case displayActionRotationCycle:
		// Kept for display builds that still let the host drive the rotation.
		cycleDisplayRotation()
	case displayActionRotationChanged:
		storeDisplayRotation(payload.Value)
	case displayActionDisplayOff:
		turnDisplayOff()
	case displayActionBacklight:
		applyBacklightPercent(payload.Value)
	case displayActionFactoryReset:
		displayLogger.Warn().Msg("factory reset requested from touchscreen")
		resetConfigFileAndReboot()
	case displayActionNetworkReset:
		displayLogger.Info().Msg("network reset requested from touchscreen")
		if err := rpcRestartDHCPClient(); err != nil {
			displayLogger.Warn().Err(err).Msg("failed to restart dhcp client")
			flashActionFailure(displayActionNetworkReset)
		}
	case displayActionUsbReinit:
		displayLogger.Info().Msg("usb reinit requested from touchscreen")
		if err := rpcReinitializeUsbGadget(); err != nil {
			displayLogger.Warn().Err(err).Msg("failed to reinitialize usb gadget")
			flashActionFailure(displayActionUsbReinit)
		}
	default:
		displayLogger.Warn().Str("action", payload.Action).Msg("unknown display action")
	}
}

// holdIO drives the power / reset line while the touchscreen button is pressed.
func holdIO(name string, high bool) {
	if err := rpcSetIOStatus(name, high); err != nil {
		displayLogger.Warn().Err(err).Str("io", name).Bool("high", high).Msg("failed to drive io line")
		return
	}

	ioHoldTimers.Lock()
	defer ioHoldTimers.Unlock()

	if timer, ok := ioHoldTimers.timers[name]; ok {
		timer.Stop()
		delete(ioHoldTimers.timers, name)
	}

	if !high {
		return
	}

	ioName := name
	ioHoldTimers.timers[name] = time.AfterFunc(ioHoldSafetyTimeout, func() {
		displayLogger.Warn().Str("io", ioName).Msg("io line held too long, releasing")
		if err := rpcSetIOStatus(ioName, false); err != nil {
			displayLogger.Warn().Err(err).Str("io", ioName).Msg("failed to release io line")
		}
		ioHoldTimers.Lock()
		delete(ioHoldTimers.timers, ioName)
		ioHoldTimers.Unlock()
	})
}

// storeDisplayRotation persists the orientation the display already applied on its
// own. No command is sent back: the panel is the source of truth.
func storeDisplayRotation(degrees int) {
	rotation := strconv.Itoa(degrees)

	displaySettingsMu.Lock()
	defer displaySettingsMu.Unlock()

	if config.DisplayRotation == rotation {
		return
	}

	config.DisplayRotation = rotation
	SaveConfig()
	displayLogger.Info().Str("rotation", rotation).Msg("display rotated on the touchscreen")
}

// displayRotationOrder lists the supported orientations in cycle order.
var displayRotationOrder = []string{"0", "90", "180", "270"}

// cycleDisplayRotation advances to the next orientation in displayRotationOrder.
func cycleDisplayRotation() {
	displaySettingsMu.Lock()
	defer displaySettingsMu.Unlock()

	next := displayRotationOrder[0]
	for i, rotation := range displayRotationOrder {
		if rotation == config.DisplayRotation {
			next = displayRotationOrder[(i+1)%len(displayRotationOrder)]
			break
		}
	}

	if _, err := lvDispSetRotation(next); err != nil {
		displayLogger.Warn().Err(err).Str("rotation", next).Msg("failed to rotate display")
		return
	}

	config.DisplayRotation = next
	if err := SaveConfig(); err != nil {
		displayLogger.Warn().Err(err).Msg("failed to save display rotation")
	}
	displayLogger.Info().Str("rotation", next).Msg("display rotated from touchscreen")
}

// turnDisplayOff fades the backlight out, the next touch wakes it up again
// through watchTsEvents once the anti mis-touch guard has elapsed.
func turnDisplayOff() {
	displaySettingsMu.Lock()
	defer displaySettingsMu.Unlock()

	switchDisplayOff()
}

// toggleDisplayPower switches the panel between dark and the configured
// brightness. It returns true when the display is now lit.
func toggleDisplayPower() bool {
	displaySettingsMu.Lock()
	defer displaySettingsMu.Unlock()

	if backlightState == 2 {
		wakeDisplay(true)
		return true
	}

	switchDisplayOff()
	return false
}

// notifyDisplayState tells the web UI whether the panel is lit, so a one click
// display off button does not go out of sync with the touchscreen.
func notifyDisplayState(on bool) {
	if currentSession == nil {
		return
	}

	writeJSONRPCEvent("displayState", map[string]interface{}{
		"on": on,
	}, currentSession)
}

// displayBacklightPercent converts a hardware brightness level to slider percent.
func displayBacklightPercent(level int) int {
	if level <= 0 {
		return 0
	}
	percent := level * 100 / backlightFullScale
	if percent > 100 {
		percent = 100
	}
	return percent
}

// backlightMinPercent is the darkest position the sliders allow. Zero is
// reserved for the dedicated display off action so the panel can always be
// brought back up with a touch.
const backlightMinPercent = 10

// displayBacklightLevel converts slider percent to a hardware brightness level.
func displayBacklightLevel(percent int) int {
	if percent <= 0 {
		return 0
	}
	if percent > 100 {
		percent = 100
	}
	return percent * backlightFullScale / 100
}

// pushBacklightToDisplay keeps the touchscreen slider in sync with the config.
func pushBacklightToDisplay(percent int) {
	_, err := CallDisplayCtrlAction("lv_slider_set_value", map[string]interface{}{
		"obj":   "Settings_Backlight_Slider",
		"value": percent,
	})
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to sync backlight slider")
	}
}

// notifyBacklightChanged tells the web UI the brightness the touchscreen just set,
// so an open settings page does not keep showing the previous value.
func notifyBacklightChanged(percent int) {
	writeJSONRPCEvent("backlightChanged", map[string]interface{}{
		"max_brightness": percent,
	}, currentSession)
}

// actionFailureTiles maps the one shot actions to the touchscreen tile that
// triggered them, along with that tile's resting background colour.
var actionFailureTiles = map[string]struct {
	obj          string
	defaultColor string
}{
	displayActionNetworkReset: {obj: "NetDetail_Reset_Cont", defaultColor: "232124"},
	displayActionUsbReinit:    {obj: "UsbDetail_Reset_Cont", defaultColor: "232124"},
}

// flashActionFailure tints the tile that requested a failing action red for a
// moment. The touchscreen plays its success animation unconditionally, so
// without this the user would assume the action worked.
func flashActionFailure(action string) {
	tile, ok := actionFailureTiles[action]
	if !ok {
		return
	}

	go func() {
		setTileColor(tile.obj, "ff3b30")
		time.Sleep(800 * time.Millisecond)
		setTileColor(tile.obj, tile.defaultColor)
	}()
}

func setTileColor(obj, color string) {
	_, err := CallDisplayCtrlAction("lv_obj_set_style_bg_color", map[string]interface{}{
		"obj":   obj,
		"color": color,
	})
	if err != nil {
		displayLogger.Debug().Err(err).Str("obj", obj).Msg("failed to tint action tile")
	}
}

// displayConfiguredBrightnessLevel converts the configured percentage to the raw
// level the backlight class expects.
func displayConfiguredBrightnessLevel() int {
	return displayBacklightLevel(config.DisplayBrightnessPercent)
}

// applyBacklightPercent applies a backlight change requested from the touchscreen
// slider and stores it as the new maximum brightness, mirroring the web UI setting.
func applyBacklightPercent(percent int) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	displaySettingsMu.Lock()
	config.DisplayBrightnessPercent = percent
	if err := SaveConfig(); err != nil {
		displayLogger.Warn().Err(err).Msg("failed to save backlight setting")
	}
	displaySettingsMu.Unlock()

	// Let an open web UI settings page follow the touchscreen slider.
	notifyBacklightChanged(percent)

	if percent == 0 {
		switchDisplayOff()
		return
	}

	// wakeDisplay writes the new brightness and restarts the dim / off tickers.
	wakeDisplay(true)
}

// resetConfigFileAndReboot deletes the persisted config, syncs the filesystem and
// reboots the device. It is triggered by the touchscreen when the boot key was
// held long enough on the factory reset screen.
func resetConfigFileAndReboot() {
	resetFirewallForFactory()

	if err := os.Remove(configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		displayLogger.Error().Err(err).Str("path", configPath).Msg("failed to delete config file")
	} else {
		displayLogger.Warn().Str("path", configPath).Msg("config file deleted")
	}

	unix.Sync()
	time.Sleep(200 * time.Millisecond)

	if err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART); err != nil {
		displayLogger.Error().Err(err).Msg("syscall reboot failed, trying /sbin/reboot")
		_ = exec.Command("/sbin/reboot", "-f").Run()
		_ = exec.Command("reboot", "-f").Run()
	}
}

// pushMainNetworkState updates the network card on the main screen.
func pushMainNetworkState() {
	connected := false
	text := "Disconnected"

	if networkState != nil {
		connected = networkState.IsUp()
		switch {
		case !connected:
			text = "Disconnected"
		case networkState.HasIPAssigned() && networkState.IPv4() != nil:
			text = networkState.IPv4String()
		default:
			text = "Connected"
		}
	}

	_, err := CallDisplayCtrlAction("lv_obj_set_main_network", map[string]interface{}{
		"connected": connected,
		"text":      text,
	})
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to update main network card")
	}
}

// humanSize renders a byte count for the slow-panel detail screens.
func humanSize(n int64) string {
	if n <= 0 {
		return "--"
	}
	units := []string{"B", "KB", "MB", "GB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", f, units[i])
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// pushNetworkDetail updates the network detail screen (IP / MAC / state).
func pushNetworkDetail() {
	ip := ""
	mac := ""
	connected := false

	if networkState != nil {
		connected = networkState.IsUp()
		mac = networkState.MACString()
		if connected && networkState.HasIPAssigned() {
			ip = networkState.IPv4String()
		}
	}

	_, err := CallDisplayCtrlAction("lv_obj_set_network_detail", map[string]interface{}{
		"ip":        ip,
		"mac":       mac,
		"connected": connected,
	})
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to update network detail")
	}
}

// pushUsbDetail updates the USB detail screen (image name / size / mount state).
func pushUsbDetail() {
	name := ""
	size := "--"
	mounted := false

	state, err := rpcGetVirtualMediaState()
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to read virtual media state")
	}
	if state != nil {
		mounted = true
		switch {
		case state.Filename != "":
			name = state.Filename
		case state.Path != "":
			name = filepath.Base(state.Path)
		case state.URL != "":
			name = state.URL
		}
		size = humanSize(state.Size)
	}

	_, err = CallDisplayCtrlAction("lv_obj_set_usb_detail", map[string]interface{}{
		"name":    name,
		"size":    size,
		"mounted": mounted,
	})
	if err != nil {
		displayLogger.Warn().Err(err).Msg("failed to update usb detail")
	}
}
