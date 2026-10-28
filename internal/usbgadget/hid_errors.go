package usbgadget

import (
	"errors"
	"syscall"
)

// ErrHIDDeviceNotConfigured marks a HID write that found no device file because
// the owning function is switched off in the current gadget configuration. The
// node is absent by design in that state, so callers must stay silent instead of
// reporting a fault and asking for a gadget rebuild.
var ErrHIDDeviceNotConfigured = errors.New("hid device not configured")

// IsHIDTemporarilyUnavailableError matches transient HID gadget errors that
// can happen while the USB gadget is detaching/rebinding.
func IsHIDTemporarilyUnavailableError(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, syscall.ENXIO) || // no such device or address
		errors.Is(err, syscall.ESHUTDOWN) || // transport endpoint shutdown
		errors.Is(err, syscall.ENOTCONN) || // transport endpoint is not connected
		errors.Is(err, syscall.EPIPE) // broken pipe
}

// IsHIDBenignError reports HID errors that must not be logged as faults or fed to
// the missing-HID watchdog: the transport is momentarily unavailable, or the
// function is switched off and therefore has no device file at all.
func IsHIDBenignError(err error) bool {
	return IsHIDTemporarilyUnavailableError(err) || errors.Is(err, ErrHIDDeviceNotConfigured)
}
