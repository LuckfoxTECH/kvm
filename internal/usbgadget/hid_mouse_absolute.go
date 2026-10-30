package usbgadget

import (
	"fmt"
	"os"
)

const (
	// absHidReportLength is the report_length of the absolute HID function: the
	// report ID plus the buttons, X, Y, wheel and AC Pan fields. It holds exactly one
	// collection, so every write fills it and no padding is needed.
	//
	// The pen and the touch screen live on their own HID function (hid.usb3); see
	// hid_pen_touch.go for why they must not share this interface.
	absHidReportLength = 8

	// absReportIDMouse is the report ID of the only collection of this function, the
	// absolute pointer that desktop hosts turn into a cursor.
	absReportIDMouse = 1
)

var absoluteMouseConfig = gadgetConfigItem{
	order:      1001,
	device:     "hid.usb1",
	path:       []string{"functions", "hid.usb1"},
	configPath: []string{"hid.usb1"},
	attrs: gadgetAttributes{
		"protocol":      "2",
		"subclass":      "0",
		"report_length": "8",
	},
	reportDesc: absoluteMouseReportDesc,
}

// mouseCollectionDesc is Report ID 1: the absolute pointer that desktop hosts
// turn into a cursor with buttons and a wheel.
var mouseCollectionDesc = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop Ctrls)
	0x09, 0x02, // Usage (Mouse)
	0xA1, 0x01, // Collection (Application)

	0x85, 0x01, //     Report ID (1)
	0x09, 0x01, //     Usage (Pointer)
	0xA1, 0x00, //     Collection (Physical)
	0x05, 0x09, //         Usage Page (Button)
	0x19, 0x01, //         Usage Minimum (0x01)
	0x29, 0x05, //         Usage Maximum (0x05)
	0x15, 0x00, //         Logical Minimum (0)
	0x25, 0x01, //         Logical Maximum (1)
	0x75, 0x01, //         Report Size (1)
	0x95, 0x05, //         Report Count (5)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0x95, 0x01, //         Report Count (1)
	0x75, 0x03, //         Report Size (3)
	0x81, 0x03, //         Input (Cnst, Var, Abs)
	0x05, 0x01, //         Usage Page (Generic Desktop Ctrls)
	0x09, 0x30, //         Usage (X)
	0x09, 0x31, //         Usage (Y)
	0x16, 0x00, 0x00, //         Logical Minimum (0)
	0x26, 0xFF, 0x7F, //         Logical Maximum (32767)
	0x36, 0x00, 0x00, //         Physical Minimum (0)
	0x46, 0xFF, 0x7F, //         Physical Maximum (32767)
	0x75, 0x10, //         Report Size (16)
	0x95, 0x02, //         Report Count (2)
	0x81, 0x02, //         Input (Data, Var, Abs)

	// Wheel and AC Pan share the movement report. Hosts (macOS in particular) only
	// follow the wheel of a mouse application collection when it arrives in that
	// collection's primary report; a separate report ID carrying the wheel alone
	// is accepted by the hardware but never turned into a scroll event.
	0x09, 0x38, //         Usage (Wheel)
	0x15, 0x81, //         Logical Minimum (-127)
	0x25, 0x7F, //         Logical Maximum (127)
	0x35, 0x00, //         Physical Minimum (0) = Reset Physical Minimum
	0x45, 0x00, //         Physical Maximum (0) = Reset Physical Maximum
	0x75, 0x08, //         Report Size (8)
	0x95, 0x01, //         Report Count (1)
	0x81, 0x06, //         Input (Data, Var, Rel)
	0x05, 0x0C, //         Usage Page (Consumer)
	0x0A, 0x38, 0x02, //         Usage (AC Pan)
	0x15, 0x81, //         Logical Minimum (-127)
	0x25, 0x7F, //         Logical Maximum (127)
	0x75, 0x08, //         Report Size (8)
	0x95, 0x01, //         Report Count (1)
	0x81, 0x06, //         Input (Data, Var, Rel)

	0xC0, //     End Collection
	0xC0, // End Collection
}

// absoluteMouseReportDesc is the descriptor of the absolute HID function. It holds
// a single mouse collection: anything else here would change how hosts group the
// interface, and a digitizer usage in particular makes Linux hand the whole
// interface to hid-multitouch and drop the cursor (see hid_pen_touch.go).
var absoluteMouseReportDesc = mouseCollectionDesc

// absHidReport frames a payload as a report of the absolute HID function: report
// ID first, then the payload, then zeros up to absHidReportLength.
func absHidReport(reportID byte, payload []byte) []byte {
	report := make([]byte, absHidReportLength)
	report[0] = reportID
	copy(report[1:], payload)
	return report
}

func (u *UsbGadget) absMouseWriteHidFile(data []byte) error {
	if !u.enabledDevices.AbsoluteMouse {
		// Switched off, so /dev/hidg1 is absent by design rather than missing.
		return ErrHIDDeviceNotConfigured
	}
	if u.absMouseHidFile == nil {
		var err error
		hidNodePath := u.HidgNodePath("absolute_mouse")
		u.absMouseHidFile, err = os.OpenFile(hidNodePath, os.O_RDWR, 0666)
		if err != nil {

			if isMissingHidNodeError(err) {
				if u.hidNodeGenuinelyMissing() {
					u.log.Error().
						Str("device", hidNodePath).
						Str("device_name", "absolute_mouse").
						Err(err).
						Msg("HID device file missing, gadget may need reinitialization")
					if u.onHidDeviceMissing != nil {
						(*u.onHidDeviceMissing)("absolute_mouse", err)
					}
				} else {
					u.log.Debug().
						Str("device", hidNodePath).
						Err(err).
						Msg("hidg1 not present yet; host has not finished enumeration")
				}
			}
			return fmt.Errorf("failed to open %s: %w", hidNodePath, err)
		}
	}

	_, err := u.absMouseHidFile.Write(data)
	if err != nil {
		u.logWithSuppression("absMouseWriteHidFile", 100, u.log, err, "failed to write to hidg1")
		u.absMouseHidFile.Close()
		u.absMouseHidFile = nil
		return err
	}
	u.resetLogSuppressionCounter("absMouseWriteHidFile")
	return nil
}

// writeAbsReport writes one report of the absolute HID function and marks the
// gadget as recently used. Callers that also track pointer state must hold
// absMouseLock themselves instead of calling this.
func (u *UsbGadget) writeAbsReport(report []byte) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.absMouseLock.Lock()
	defer u.absMouseLock.Unlock()

	if err := u.absMouseWriteHidFile(report); err != nil {
		return err
	}
	u.resetUserInputTime()
	return nil
}

func (u *UsbGadget) AbsMouseReport(x, y int, buttons uint8) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.absMouseLock.Lock()
	defer u.absMouseLock.Unlock()

	err := u.absMouseWriteHidFile(absHidReport(absReportIDMouse, []byte{
		buttons,       // Buttons
		uint8(x),      // X Low Byte
		uint8(x >> 8), // X High Byte
		uint8(y),      // Y Low Byte
		uint8(y >> 8), // Y High Byte
		0,             // Wheel
		0,             // AC Pan
	}))
	if err != nil {
		return err
	}

	u.absMouseX = x
	u.absMouseY = y
	u.absMouseButtons = buttons

	u.resetUserInputTime()
	return nil
}

// GetAbsMousePosition returns the last absolute mouse position reported to the host,
// in HID logical units (0-32767).
func (u *UsbGadget) GetAbsMousePosition() (x, y int) {
	u.absMouseLock.Lock()
	defer u.absMouseLock.Unlock()

	return u.absMouseX, u.absMouseY
}

// HasAbsoluteMouse reports whether the absolute mouse HID function is enabled.
func (u *UsbGadget) HasAbsoluteMouse() bool {
	return u.enabledDevices.AbsoluteMouse
}

func (u *UsbGadget) AbsMouseWheelReport(wheelY, wheelX int8) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.absMouseLock.Lock()
	defer u.absMouseLock.Unlock()

	// Only send a report if the value is non-zero
	if wheelY == 0 && wheelX == 0 {
		return nil
	}

	// The wheel shares the movement report, so the last position and button state
	// have to be repeated: a report without them would move the cursor.
	x, y := u.absMouseX, u.absMouseY
	err := u.absMouseWriteHidFile(absHidReport(absReportIDMouse, []byte{
		u.absMouseButtons, // Buttons
		uint8(x),          // X Low Byte
		uint8(x >> 8),     // X High Byte
		uint8(y),          // Y Low Byte
		uint8(y >> 8),     // Y High Byte
		byte(wheelY),      // Wheel (signed)
		byte(wheelX),      // AC Pan (signed)
	}))

	u.resetUserInputTime()
	return err
}
