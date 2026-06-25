package kvm

import (
	"fmt"
	"os"
	"sync"
	"time"

	"kvm/internal/usbgadget"
)

var gadget *usbgadget.UsbGadget
var reinitLock sync.Mutex
var isReinitializing bool

// initUsbGadget initializes the USB gadget.
// call it only after the config is loaded.
func initUsbGadget() {
	resp, _ := rpcGetSDMountStatus()
	if resp.Status == SDMountOK {
		if err := writeUmtprdConfFile(true); err != nil {
			usbLogger.Error().Err(err).Msg("failed to write umtprd conf file")
		}
	} else {
		if err := writeUmtprdConfFile(false); err != nil {
			usbLogger.Error().Err(err).Msg("failed to write umtprd conf file")
		}
	}

	gadget = usbgadget.NewUsbGadget(
		"kvm",
		config.UsbDevices,
		config.UsbConfig,
		usbLogger,
	)

	go func() {
		for {
			checkUSBState()
			time.Sleep(2500 * time.Millisecond)
		}
	}()

	gadget.SetOnKeyboardStateChange(func(state usbgadget.KeyboardState) {
		if currentSession != nil {
			writeJSONRPCEvent("keyboardLedState", state, currentSession)
		}
	})

	// Set callback for HID device missing errors
	gadget.SetOnHidDeviceMissing(func(device string, err error) {
		usbLogger.Error().
			Str("device", device).
			Err(err).
			Msg("HID device missing, sending notification to client")

		if currentSession != nil {
			writeJSONRPCEvent("hidDeviceMissing", map[string]interface{}{
				"device": device,
				"error":  err.Error(),
			}, currentSession)
		}

		go func() {
			usbLogger.Info().Str("device", device).Msg("Attempting to reinitialize USB gadget due to missing HID device")
			if err := rpcReinitializeUsbGadget(); err != nil {
				usbLogger.Error().Err(err).Msg("Failed to auto-reinitialize USB gadget")
			}
		}()
	})

	// open the keyboard hid file to listen for keyboard events
	if err := gadget.OpenKeyboardHidFile(); err != nil {
		usbLogger.Error().Err(err).Msg("failed to open keyboard hid file")
	}
}

func initSystemInfo() {
	if !config.AutoMountSystemInfo {
		return
	}

	go func() {
		for {
			if !networkState.HasIPAssigned() {
				vpnLogger.Warn().Msg("waiting for network get IPv4 address, will retry in 3 seconds")
				time.Sleep(3 * time.Second)
				continue
			} else {
				break
			}
		}
		err := writeSystemInfoImg()
		if err != nil {
			usbLogger.Error().Err(err).Msg("failed to create system_info.img")
		}

		mediaState, _ := rpcGetVirtualMediaState()
		if mediaState != nil && mediaState.Filename == "system_info.img" {
			usbLogger.Error().Err(err).Msg("system_info.img is busy")
		} else if mediaState == nil || mediaState.Filename == "" {
			err = rpcMountWithStorage("system_info.img", Disk)
			if err != nil {
				usbLogger.Error().Err(err).Msg("failed to mount system_info.img")
			}
		}
	}()
}

func rpcKeyboardReport(modifier uint8, keys []uint8) error {
	return gadget.KeyboardReport(modifier, keys)
}

func rpcKeypressReport(key uint8, press bool) error {
	return gadget.KeypressReport(key, press)
}

func rpcKeypressKeepAlive() error {
	return gadget.KeypressKeepAlive()
}

func rpcAbsMouseReport(x, y int, buttons uint8) error {
	return gadget.AbsMouseReport(x, y, buttons)
}

func rpcRelMouseReport(dx, dy int8, buttons uint8) error {
	return gadget.RelMouseReport(dx, dy, buttons, 0)
}

func rpcWheelReport(wheelY int8, mouseMode string) error {
	if mouseMode == "relative" {
		return gadget.RelMouseReport(0, 0, 0, wheelY)
	}
	return gadget.AbsMouseWheelReport(wheelY)
}

func rpcGetKeyboardLedState() (state usbgadget.KeyboardState) {
	return gadget.GetKeyboardState()
}

var usbState = "unknown"

func rpcGetUSBState() (state string) {
	return gadget.GetUsbState(config.UsbEnhancedDetection)
}

func triggerUSBStateUpdate() {
	go func() {
		if currentSession == nil {
			usbLogger.Info().Msg("No active RPC session, skipping update state update")
			return
		}
		writeJSONRPCEvent("usbState", usbState, currentSession)
	}()
}

func checkUSBState() {
	newState := gadget.GetUsbState(config.UsbEnhancedDetection)
	if newState == usbState {
		return
	}
	usbState = newState

	usbLogger.Info().Str("from", usbState).Str("to", newState).Msg("USB state changed")
	requestDisplayUpdate(true)
	triggerUSBStateUpdate()
}

func rpcSendUsbWakeupSignal() error {
	err := os.WriteFile("/sys/class/udc/ffb00000.usb/srp", []byte("1"), 0644)
	if err != nil {
		return err
	}
	return nil
}

// rpcReinitializeUsbGadget reinitializes the USB gadget
func rpcReinitializeUsbGadget() error {
	reinitLock.Lock()
	if isReinitializing {
		reinitLock.Unlock()
		usbLogger.Warn().Msg("USB gadget reinitialization already in progress, skipping")
		return nil
	}
	isReinitializing = true
	reinitLock.Unlock()

	defer func() {
		reinitLock.Lock()
		isReinitializing = false
		reinitLock.Unlock()
	}()

	usbLogger.Info().Msg("reinitializing USB gadget (hard)")

	if gadget == nil {
		return fmt.Errorf("USB gadget not initialized")
	}

	mediaState, _ := rpcGetVirtualMediaState()
	if mediaState != nil && (mediaState.Filename != "" || mediaState.URL != "") {
		usbLogger.Info().Interface("mediaState", mediaState).Msg("virtual media mounted, unmounting before USB reinit")
		if err := rpcUnmountImage(); err != nil {
			usbLogger.Warn().Err(err).Msg("failed to unmount virtual media before USB reinit")
		}
	}

	gadget.CloseHidFiles()

	// Recreate the gadget instance similar to program restart
	gadget = usbgadget.NewUsbGadget(
		"kvm",
		config.UsbDevices,
		config.UsbConfig,
		usbLogger,
	)

	// Reapply callbacks
	gadget.SetOnKeyboardStateChange(func(state usbgadget.KeyboardState) {
		if currentSession != nil {
			writeJSONRPCEvent("keyboardLedState", state, currentSession)
		}
	})
	gadget.SetOnHidDeviceMissing(func(device string, err error) {
		usbLogger.Error().
			Str("device", device).
			Err(err).
			Msg("HID device missing, sending notification to client")

		if currentSession != nil {
			writeJSONRPCEvent("hidDeviceMissing", map[string]interface{}{
				"device": device,
				"error":  err.Error(),
			}, currentSession)
		}

		go func() {
			usbLogger.Info().Str("device", device).Msg("Attempting to reinitialize USB gadget due to missing HID device")
			if err := rpcReinitializeUsbGadget(); err != nil {
				usbLogger.Error().Err(err).Msg("Failed to auto-reinitialize USB gadget")
			}
		}()
	})

	// Reopen keyboard HID file
	if err := gadget.OpenKeyboardHidFile(); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to open keyboard hid file after reinit")
	}

	// Force a USB state update notification
	triggerUSBStateUpdate()

	initSystemInfo()

	usbLogger.Info().Msg("USB gadget reinitialized successfully")
	return nil
}

// rpcReinitializeUsbGadgetSoft performs a lightweight refresh:
// reapply configuration and rebind without recreating the gadget instance.
func rpcReinitializeUsbGadgetSoft() error {
	usbLogger.Info().Msg("reinitializing USB gadget (soft)")

	if gadget == nil {
		return fmt.Errorf("USB gadget not initialized")
	}

	mediaState, _ := rpcGetVirtualMediaState()
	if mediaState != nil && (mediaState.Filename != "" || mediaState.URL != "") {
		usbLogger.Info().Interface("mediaState", mediaState).Msg("virtual media mounted, unmounting before USB soft reinit")
		if err := rpcUnmountImage(); err != nil {
			usbLogger.Warn().Err(err).Msg("failed to unmount virtual media before USB soft reinit")
		}
	}

	// Update gadget configuration (will rebind USB inside)
	if err := gadget.UpdateGadgetConfig(); err != nil {
		usbLogger.Error().Err(err).Msg("failed to soft reinitialize USB gadget")
		return fmt.Errorf("failed to soft reinitialize USB gadget: %w", err)
	}

	// Reopen keyboard HID file
	if err := gadget.OpenKeyboardHidFile(); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to reopen keyboard hid file after soft reinit")
	}

	// Force a USB state update notification
	triggerUSBStateUpdate()

	usbLogger.Info().Msg("USB gadget soft reinitialized successfully")
	return nil
}
