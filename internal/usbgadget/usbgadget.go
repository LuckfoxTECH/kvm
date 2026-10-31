// Package usbgadget provides a high-level interface to manage USB gadgets
// THIS PACKAGE IS FOR INTERNAL USE ONLY AND ITS API MAY CHANGE WITHOUT NOTICE
package usbgadget

import (
	"context"
	"os"
	"path"
	"sync"
	"time"

	"kvm/internal/logging"

	"github.com/rs/zerolog"
)

// Devices is a struct that represents the USB devices that can be enabled on a USB gadget.
type Devices struct {
	AbsoluteMouse bool `json:"absolute_mouse"`
	RelativeMouse bool `json:"relative_mouse"`
	Keyboard      bool `json:"keyboard"`
	MassStorage   bool `json:"mass_storage"`
	Mtp           bool `json:"mtp"`
	Audio         bool `json:"audio"`
}

// Config is a struct that represents the customizations for a USB gadget.
// TODO: rename to something else that won't confuse with the USB gadget configuration
type Config struct {
	VendorId     string `json:"vendor_id"`
	ProductId    string `json:"product_id"`
	SerialNumber string `json:"serial_number"`
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`

	strictMode bool // when it's enabled, all warnings will be converted to errors
	isEmpty    bool
}

var defaultUsbGadgetDevices = Devices{
	AbsoluteMouse: true,
	RelativeMouse: true,
	Keyboard:      true,
	MassStorage:   true,
}

// UsbGadget is a struct that represents a USB gadget.
type UsbGadget struct {
	name          string
	udc           string
	kvmGadgetPath string
	configC1Path  string

	// initErr keeps the reason Init failed. Callers that report success used to
	// discard it, so a gadget that never reached the host was announced as rebuilt.
	initErr error

	configMap    map[string]gadgetConfigItem
	customConfig Config

	configLock sync.Mutex

	// hidLifecycle is held for reading by every HID access and exclusively by a
	// rebind, so a controller reset never overlaps an in-flight HID write.
	hidLifecycle sync.RWMutex
	hidOpens     hidOpenTracker

	keyboardHidFile *os.File
	keyboardLock    sync.Mutex
	absMouseHidFile *os.File
	absMouseLock    sync.Mutex
	absMouseX       int   // last absolute X reported to the host, HID logical units (0-32767)
	absMouseY       int   // last absolute Y reported to the host, HID logical units (0-32767)
	absMouseButtons uint8 // last button bitmask reported through the absolute mouse
	relMouseHidFile *os.File
	relMouseLock    sync.Mutex

	penTouchHidFile *os.File
	penTouchLock    sync.Mutex

	keyboardState       KeyboardState
	keyboardStateLock   sync.Mutex
	keyboardStateCtx    context.Context
	keyboardStateCancel context.CancelFunc

	enabledDevices Devices

	strictMode bool // only intended for testing for now

	absMouseAccumulatedWheelY float64

	lastUserInput time.Time

	tx     *UsbGadgetTransaction
	txLock sync.Mutex

	onKeyboardStateChange *func(state KeyboardState)
	onHidDeviceMissing    *func(device string, err error)

	keysDownState        KeysDownState
	onKeysDownChange     *func(state KeysDownState)
	kbdAutoReleaseLock   sync.Mutex
	kbdAutoReleaseTimers map[byte]*kbdAutoReleaseTimer

	log *zerolog.Logger

	logSuppressionCounter map[string]int
	logLock               sync.Mutex
}

const configFSPath = "/sys/kernel/config"
const gadgetPath = "/sys/kernel/config/usb_gadget"

const functionFSPath = "/dev/ffs-mtp"
const umtprdPath = "/usr/sbin/umtprd"

var defaultLogger = logging.GetSubsystemLogger("usbgadget")

// NewUsbGadget creates a new UsbGadget.
func NewUsbGadget(name string, enabledDevices *Devices, config *Config, logger *zerolog.Logger) *UsbGadget {
	return newUsbGadget(name, defaultGadgetConfig, enabledDevices, config, logger)
}

func newUsbGadget(name string, configMap map[string]gadgetConfigItem, enabledDevices *Devices, config *Config, logger *zerolog.Logger) *UsbGadget {
	if logger == nil {
		logger = defaultLogger
	}

	if enabledDevices == nil {
		enabledDevices = &defaultUsbGadgetDevices
	}

	if config == nil {
		config = &Config{isEmpty: true}
	}

	keyboardCtx, keyboardCancel := context.WithCancel(context.Background())

	g := &UsbGadget{
		name:                name,
		kvmGadgetPath:       path.Join(gadgetPath, name),
		configC1Path:        path.Join(gadgetPath, name, "configs/c.1"),
		configMap:           configMap,
		customConfig:        *config,
		configLock:          sync.Mutex{},
		keyboardLock:        sync.Mutex{},
		absMouseLock:        sync.Mutex{},
		relMouseLock:        sync.Mutex{},
		penTouchLock:        sync.Mutex{},
		txLock:              sync.Mutex{},
		keyboardStateCtx:    keyboardCtx,
		keyboardStateCancel: keyboardCancel,
		keyboardState:       KeyboardState{},
		enabledDevices:      *enabledDevices,
		lastUserInput:       time.Now(),
		log:                 logger,

		strictMode: config.strictMode,

		logSuppressionCounter: make(map[string]int),
		kbdAutoReleaseTimers:  make(map[byte]*kbdAutoReleaseTimer),

		absMouseAccumulatedWheelY: 0,
	}
	if err := g.Init(); err != nil {
		g.initErr = err
		logger.Error().Err(err).Msg("failed to init USB gadget (will retry later)")
	}

	return g
}

// InitError reports why the gadget could not be built, or nil when it was built.
func (u *UsbGadget) InitError() error {
	return u.initErr
}

// HasKeyboard reports whether the keyboard HID function is part of the current
// gadget configuration. Recovery paths use it to avoid probing a function that
// was intentionally disabled, which would report a false failure.
func (u *UsbGadget) HasKeyboard() bool {
	u.configLock.Lock()
	defer u.configLock.Unlock()
	return u.enabledDevices.Keyboard
}

// ResetHIDFiles closes all open HID gadget file descriptors.
// After a UDC rebind, previously open /dev/hidg* handles may point to a stale
// transport endpoint and must be reopened before use.
func (u *UsbGadget) ResetHIDFiles() {
	u.keyboardLock.Lock()
	u.closeKeyboardHidFileLocked()
	u.keyboardLock.Unlock()

	u.absMouseLock.Lock()
	if u.absMouseHidFile != nil {
		u.absMouseHidFile.Close()
		u.absMouseHidFile = nil
	}
	u.absMouseLock.Unlock()

	u.relMouseLock.Lock()
	if u.relMouseHidFile != nil {
		u.relMouseHidFile.Close()
		u.relMouseHidFile = nil
	}
	u.relMouseLock.Unlock()

	u.penTouchLock.Lock()
	if u.penTouchHidFile != nil {
		u.penTouchHidFile.Close()
		u.penTouchHidFile = nil
	}
	u.penTouchLock.Unlock()

	u.log.Info().Msg("all HID file descriptors reset")
}
