package kvm

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"kvm/internal/usbgadget"
)

var gadget *usbgadget.UsbGadget
var reinitLock sync.Mutex
var isReinitializing bool

// system_info.img generation and mounting are separate steps that run
// concurrently at boot. Auto-mount waits on this channel so the image is never
// mounted while writeSystemInfoImg is still rewriting it.
var (
	systemInfoImgReadyMu sync.Mutex
	systemInfoImgReady   chan struct{}
)

// resetSystemInfoImgReady publishes a fresh readiness channel for the current
// writeSystemInfoImg run and returns it for the writer to close on completion.
func resetSystemInfoImgReady() chan struct{} {
	systemInfoImgReadyMu.Lock()
	defer systemInfoImgReadyMu.Unlock()
	systemInfoImgReady = make(chan struct{})
	return systemInfoImgReady
}

// waitSystemInfoImgReady blocks until the current system_info.img write finishes
// or the timeout expires.
func waitSystemInfoImgReady(timeout time.Duration) {
	systemInfoImgReadyMu.Lock()
	ready := systemInfoImgReady
	systemInfoImgReadyMu.Unlock()

	if ready == nil {
		return
	}

	select {
	case <-ready:
	case <-time.After(timeout):
		usbLogger.Warn().Msg("timed out waiting for system_info.img to be written")
	}
}

var reinitStartedAt time.Time
var reinitGeneration uint64

// reinitStaleTimeout is how long a rebuild may hold the reinitialization lock
// before another rebuild treats it as hung. A rebuild that blocks forever (for
// example waiting on hidLifecycle while a write never returns) used to latch the
// flag for the whole process lifetime, so every later rebuild - automatic or
// manual - was skipped until the program was restarted.
const reinitStaleTimeout = 45 * time.Second

const (
	// maxAutoReinitAttempts bounds how many gadget rebuilds the HID watchdog may
	// trigger without a single successful HID write in between. A persistent failure
	// (for example a host that rejects the keyboard report descriptor and never
	// configures the interface) would otherwise rebuild the gadget forever and keep
	// the bus re-enumerating.
	maxAutoReinitAttempts = 3
	// autoReinitMinInterval collapses the callbacks of a single outage into one
	// rebuild. Every HID device reports itself missing separately, so without this
	// three callbacks would burn the whole budget before a rebuild ever ran.
	autoReinitMinInterval = 5 * time.Second
	// autoReinitForgetAfter drops an old streak, so two outages half an hour apart
	// cannot add up into a brake.
	autoReinitForgetAfter = 30 * time.Minute
)

type autoReinitDecision int

const (
	autoReinitNow autoReinitDecision = iota
	autoReinitSkip
	autoReinitStop
)

var (
	autoReinitLock     sync.Mutex
	autoReinitAttempts int
	autoReinitLast     time.Time
)

// decideAutoReinit reports whether the HID watchdog may start another rebuild, and
// consumes one attempt when it may.
func decideAutoReinit() autoReinitDecision {
	autoReinitLock.Lock()
	defer autoReinitLock.Unlock()

	now := time.Now()
	if !autoReinitLast.IsZero() {
		if now.Sub(autoReinitLast) < autoReinitMinInterval {
			return autoReinitSkip
		}
		if now.Sub(autoReinitLast) > autoReinitForgetAfter {
			autoReinitAttempts = 0
		}
	}

	if autoReinitAttempts >= maxAutoReinitAttempts {
		return autoReinitStop
	}

	autoReinitAttempts++
	autoReinitLast = now
	return autoReinitNow
}

// resetAutoReinitBrake clears the streak. A HID write that reached the host proves
// the interface is configured again, and a manual rebuild is the way out when the
// brake did engage.
func resetAutoReinitBrake() {
	autoReinitLock.Lock()
	defer autoReinitLock.Unlock()

	autoReinitAttempts = 0
	autoReinitLast = time.Time{}
}

// onHidDeviceMissing notifies the client and, while the watchdog still has budget,
// rebuilds the gadget.
func onHidDeviceMissing(device string, err error) {
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

	switch decideAutoReinit() {
	case autoReinitSkip:
		usbLogger.Debug().Str("device", device).Msg("USB gadget rebuild already covers this outage, skipping HID watchdog request")
	case autoReinitStop:
		usbLogger.Error().
			Str("device", device).
			Int("attempts", maxAutoReinitAttempts).
			Msg("HID still missing after repeated USB gadget rebuilds, giving up until a HID write succeeds or the gadget is rebuilt manually")
	default:
		go func() {
			usbLogger.Info().Str("device", device).Msg("Attempting to reinitialize USB gadget due to missing HID device")
			if err := rpcReinitializeUsbGadget(); err != nil {
				usbLogger.Error().Err(err).Msg("Failed to auto-reinitialize USB gadget")
			}
		}()
	}
}

// initUsbGadget initializes the USB gadget.
// call it only after the config is loaded.
func initUsbGadget() {
	// report_desc is only read when the keyboard function is bound, so the
	// descriptor the persisted arrangement needs has to be selected before the
	// gadget is created.
	usbgadget.ApplyKeyboardReportDescForFormFactor(config.KeyboardFormFactor)

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

	gadget.SetOnKeyboardStateChange(func(state usbgadget.KeyboardState) {
		if currentSession != nil {
			writeJSONRPCEvent("keyboardLedState", state, currentSession)
		}
	})

	// Device-truth keyboard state: the client highlights keys from this, not from
	// what it believes it sent, so left/right modifiers stay distinct.
	gadget.SetOnKeysDownChange(func(state usbgadget.KeysDownState) {
		if currentSession != nil {
			currentSession.enqueueKeysDownState(state)
		}
	})

	// Set callback for HID device missing errors
	gadget.SetOnHidDeviceMissing(onHidDeviceMissing)

	// open the keyboard hid file to listen for keyboard events
	if err := gadget.OpenKeyboardHidFile(); err != nil && !usbgadget.IsHIDBenignError(err) {
		usbLogger.Error().Err(err).Msg("failed to open keyboard hid file")
	}

	go func() {
		for {
			checkUSBState()
			time.Sleep(2500 * time.Millisecond)
		}
	}()
}

func initSystemInfo() {
	ready := resetSystemInfoImgReady()
	go func() {
		defer close(ready)
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
	}()
}

func initAutoMountImage() {
	if config.AutoMountImage == nil || config.AutoMountImage.Filename == "" {
		return
	}

	go func() {
		// Wait for network to be ready (max 30s)
		for i := 0; i < 10; i++ {
			if !networkState.HasIPAssigned() {
				vpnLogger.Warn().Msg("waiting for network for auto-mount, will retry in 3 seconds")
				time.Sleep(3 * time.Second)
				continue
			}
			break
		}

		// Snapshot the config: it can be cleared from the UI while we wait below,
		// and dereferencing config.AutoMountImage after that would panic.
		autoMount := config.AutoMountImage
		if autoMount == nil || autoMount.Filename == "" {
			usbLogger.Info().Msg("auto-mount config cleared meanwhile, skipping")
			return
		}

		// If source is SD, wait for SD card to be available (max 30s)
		if autoMount.Source == "sd" {
			sdReady := false
			for i := 0; i < 10; i++ {
				sdStatus, err := rpcGetSDMountStatus()
				if err == nil && sdStatus.Status == SDMountOK {
					sdReady = true
					break
				}
				usbLogger.Warn().Int("attempt", i+1).Msg("waiting for SD card for auto-mount")
				time.Sleep(3 * time.Second)
			}
			if !sdReady {
				usbLogger.Warn().Msg("SD card not available for auto-mount after 30s, skipping")
				return
			}
		}

		// Check if there's already a mounted image
		mediaState, _ := rpcGetVirtualMediaState()
		if mediaState != nil && mediaState.Filename != "" {
			usbLogger.Info().Msgf("auto-mount skipped: %s is already mounted", mediaState.Filename)
			return
		}

		// system_info.img is regenerated on every boot by writeSystemInfoImg; wait for
		// that to finish so the host never sees a half-written image.
		if autoMount.Filename == systemInfoImageFilename {
			waitSystemInfoImgReady(60 * time.Second)
		}

		// Determine mode from file extension
		mode := Disk
		if strings.HasSuffix(strings.ToLower(autoMount.Filename), ".iso") {
			mode = CDROM
		}

		// Retry briefly: system_info.img may not be on disk yet when the network
		// comes up before the image writer finished.
		const autoMountAttempts = 5
		var err error
		for attempt := 0; attempt < autoMountAttempts; attempt++ {
			if attempt > 0 {
				time.Sleep(3 * time.Second)
			}
			if autoMount.Source == "sd" {
				err = rpcMountWithSDStorage(autoMount.Filename, mode)
			} else {
				err = rpcMountWithStorage(autoMount.Filename, mode)
			}
			if err == nil {
				break
			}
			// Only a missing file is worth retrying; anything else is a hard error.
			if !strings.Contains(err.Error(), "file does not exist") && !strings.Contains(err.Error(), "no such file") {
				break
			}
			usbLogger.Warn().Err(err).Int("attempt", attempt+1).Msg("auto-mount failed, retrying")
		}
		if err != nil {
			usbLogger.Error().Err(err).Msgf("failed to auto-mount %s", autoMount.Filename)
			// Clear auto-mount config if file not found
			if strings.Contains(err.Error(), "file does not exist") || strings.Contains(err.Error(), "no such file") {
				usbLogger.Warn().Msg("auto-mount image not found, clearing auto-mount config")
				config.AutoMountImage = nil
				SaveConfig()
			}
		} else {
			usbLogger.Info().Msgf("auto-mounted %s as %s from %s", autoMount.Filename, mode, autoMount.Source)
		}
	}()
}

// rpcHidReport runs a HID write, skipping it while the gadget is detached and
// swallowing the transient errors produced by detach/rebind.
func rpcHidReport(fn func() error) error {
	if !usbReadyForHidReports() {
		return nil
	}

	err := fn()
	if err != nil {
		if usbgadget.IsHIDTemporarilyUnavailableError(err) {
			return nil
		}
		return err
	}

	// The write reached the host, so the interface is configured and whatever made
	// HID disappear is over.
	resetAutoReinitBrake()
	return nil
}

func rpcKeyboardReport(modifier uint8, keys []uint8) error {
	return rpcHidReport(func() error { return gadget.KeyboardReport(modifier, keys) })
}

func rpcKeypressReport(key uint8, press bool) error {
	return rpcHidReport(func() error { return gadget.KeypressReport(key, press) })
}

func rpcKeypressKeepAlive() error {
	return rpcHidReport(func() error { return gadget.KeypressKeepAlive() })
}

func rpcAbsMouseReport(x, y int, buttons uint8) error {
	return rpcHidReport(func() error { return gadget.AbsMouseReport(x, y, buttons) })
}

func rpcRelMouseReport(dx, dy int8, buttons uint8) error {
	return rpcHidReport(func() error { return gadget.RelMouseReport(dx, dy, buttons, 0) })
}

// rpcWheelReport sends a wheel report on whichever mouse function the gadget
// actually enabled, so the host never receives a report for a missing device.
// wheelY scrolls vertically, wheelX horizontally.
func rpcWheelReport(wheelY int8, wheelX int8) error {
	return rpcHidReport(func() error {
		if gadget.HasAbsoluteMouse() {
			return gadget.AbsMouseWheelReport(wheelY, wheelX)
		}
		return gadget.RelMouseWheelReport(wheelY, wheelX)
	})
}

func rpcGetKeyboardLedState() (state usbgadget.KeyboardState) {
	return gadget.GetKeyboardState()
}

const (
	usbStateNotAttached = "not attached"
	usbStateUnknown     = "unknown"
)

var (
	usbStateLock sync.RWMutex
	usbState     = usbStateUnknown
)

func getUSBStateCached() string {
	usbStateLock.RLock()
	defer usbStateLock.RUnlock()
	return usbState
}

// usbReadyForHidReports reports whether the gadget is enumerated and able to
// accept HID writes. Writing while detached only produces EPIPE noise.
func usbReadyForHidReports() bool {
	state := getUSBStateCached()
	return state != usbStateNotAttached && state != usbStateUnknown
}

func rpcGetUSBState() (state string) {
	return gadget.GetUsbState(config.UsbEnhancedDetection)
}

func triggerUSBStateUpdate() {
	state := getUSBStateCached()
	go func() {
		if currentSession == nil {
			usbLogger.Info().Msg("No active RPC session, skipping update state update")
			return
		}
		writeJSONRPCEvent("usbState", state, currentSession)
	}()
}

// ledRestoreBackoff throttles the LED-listener restore below. Opening the HID node
// can block for the full open timeout, and the state poll only runs every 2.5s, so
// retrying on every tick would both flood the log with a failure chain and starve
// the gadget rebind that has to run to bring the node back.
const ledRestoreBackoff = 30 * time.Second

var ledRestoreLastAttempt time.Time

func checkUSBState() {
	newState := gadget.GetUsbState(config.UsbEnhancedDetection)

	// Configuration updates recreate hidg nodes after the initial LED reader
	// starts. Restore it without waiting for the next keyboard input.
	if newState == usbgadget.USBStateConfigured && gadget.HasKeyboard() {
		if time.Since(ledRestoreLastAttempt) >= ledRestoreBackoff {
			ledRestoreLastAttempt = time.Now()
			if err := gadget.OpenKeyboardHidFile(); err != nil && !usbgadget.IsHIDBenignError(err) {
				usbLogger.Warn().Err(err).Msg("failed to restore keyboard LED listener")
			}
		}
	}

	usbStateLock.Lock()
	prevState := usbState
	changed := newState != prevState
	usbState = newState
	usbStateLock.Unlock()

	if !changed {
		return
	}

	usbLogger.Info().Str("from", prevState).Str("to", newState).Msg("USB state changed")
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

// rpcReinitializeUsbGadgetManually is the RPC entry point for a rebuild. It also
// releases the automatic-rebuild brake, which is the way out of a persistent HID
// failure without restarting the device.
func rpcReinitializeUsbGadgetManually() error {
	resetAutoReinitBrake()
	// A manual rebuild is the operator's way out of a wedged state, so it must not
	// be held back by the marker of a rebuild that never returned. Taking the lock
	// over immediately beats asking the user to pick a better moment: by then the
	// device is already unusable. Bumping the generation keeps the old attempt from
	// clearing the flag on behalf of this one if it ever wakes up.
	reinitLock.Lock()
	if isReinitializing {
		usbLogger.Warn().
			Dur("age", time.Since(reinitStartedAt)).
			Msg("manual rebuild takes over an in-flight USB gadget reinitialization")
		isReinitializing = false
		reinitGeneration++
	}
	reinitLock.Unlock()

	return rpcReinitializeUsbGadget()
}

// rpcReinitializeUsbGadget reinitializes the USB gadget
func rpcReinitializeUsbGadget() error {
	reinitLock.Lock()
	if isReinitializing {
		// A rebuild that never returns must not lock the device out of every future
		// rebuild until a restart. Once the marker is older than the stale timeout,
		// take the lock over and let this attempt run.
		if time.Since(reinitStartedAt) < reinitStaleTimeout {
			reinitLock.Unlock()
			usbLogger.Warn().Msg("USB gadget reinitialization already in progress, skipping")
			return nil
		}
		usbLogger.Warn().
			Dur("age", time.Since(reinitStartedAt)).
			Msg("stale USB gadget reinitialization detected, taking over")
	}
	isReinitializing = true
	reinitStartedAt = time.Now()
	// Generations keep a hung attempt that eventually returns from clearing the
	// flag that a newer attempt has already taken over.
	reinitGeneration++
	myGeneration := reinitGeneration
	reinitLock.Unlock()

	defer func() {
		reinitLock.Lock()
		if reinitGeneration == myGeneration {
			isReinitializing = false
		}
		reinitLock.Unlock()
	}()

	usbLogger.Info().Msg("reinitializing USB gadget (hard)")

	if gadget == nil {
		return fmt.Errorf("USB gadget not initialized")
	}

	mediaState, _ := rpcGetVirtualMediaState()
	if mediaState != nil && (mediaState.Filename != "" || mediaState.URL != "") {
		usbLogger.Info().Interface("mediaState", mediaState).Msg("virtual media mounted, unmounting before USB reinit")
		if err := unmountImage(false); err != nil {
			usbLogger.Warn().Err(err).Msg("failed to unmount virtual media before USB reinit")
		}
	}

	// Close stale HID file descriptors before recreating the gadget.
	gadget.ResetHIDFiles()

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
	// The keys-down callback lives on the gadget instance, so a recreated gadget
	// must carry it over or the client stops receiving device-truth key state.
	gadget.SetOnKeysDownChange(func(state usbgadget.KeysDownState) {
		if currentSession != nil {
			currentSession.enqueueKeysDownState(state)
		}
	})
	gadget.SetOnHidDeviceMissing(onHidDeviceMissing)

	// Reopen keyboard HID file
	if err := gadget.OpenKeyboardHidFile(); err != nil {
		usbLogger.Warn().Err(err).Msg("failed to open keyboard hid file after reinit")
	}

	// Force a USB state update notification
	triggerUSBStateUpdate()

	initSystemInfo()
	initAutoMountImage()

	usbLogger.Info().Msg("USB gadget reinitialized successfully")
	return nil
}
