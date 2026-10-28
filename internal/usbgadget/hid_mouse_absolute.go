package usbgadget

import (
	"fmt"
	"os"
)

var absoluteMouseConfig = gadgetConfigItem{
	order:      1001,
	device:     "hid.usb1",
	path:       []string{"functions", "hid.usb1"},
	configPath: []string{"hid.usb1"},
	attrs: gadgetAttributes{
		"protocol":      "2",
		"subclass":      "0",
		"report_length": "6",
	},
	reportDesc: absoluteMouseCombinedReportDesc,
}

var absoluteMouseCombinedReportDesc = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop Ctrls)
	0x09, 0x02, // Usage (Mouse)
	0xA1, 0x01, // Collection (Application)

	// Report ID 1: Absolute Mouse Movement
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
	0xC0, //     End Collection

	// Report ID 2: Relative Wheel Movement
	0x85, 0x02, //     Report ID (2)
	0x09, 0x38, //     Usage (Wheel)
	0x15, 0x81, //     Logical Minimum (-127)
	0x25, 0x7F, //     Logical Maximum (127)
	0x35, 0x00, //     Physical Minimum (0) = Reset Physical Minimum
	0x45, 0x00, //     Physical Maximum (0) = Reset Physical Maximum
	0x75, 0x08, //     Report Size (8)
	0x95, 0x01, //     Report Count (1)
	0x81, 0x06, //     Input (Data, Var, Rel)
	0x05, 0x0C, //     Usage Page (Consumer)
	0x0A, 0x38, 0x02, //     Usage (AC Pan)
	0x15, 0x81, //     Logical Minimum (-127)
	0x25, 0x7F, //     Logical Maximum (127)
	0x75, 0x08, //     Report Size (8)
	0x95, 0x01, //     Report Count (1)
	0x81, 0x06, //     Input (Data, Var, Rel)

	0xC0, // End Collection
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

func (u *UsbGadget) AbsMouseReport(x, y int, buttons uint8) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.absMouseLock.Lock()
	defer u.absMouseLock.Unlock()

	err := u.absMouseWriteHidFile([]byte{
		1,             // Report ID 1
		buttons,       // Buttons
		uint8(x),      // X Low Byte
		uint8(x >> 8), // X High Byte
		uint8(y),      // Y Low Byte
		uint8(y >> 8), // Y High Byte
	})
	if err != nil {
		return err
	}

	u.absMouseX = x
	u.absMouseY = y

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

	err := u.absMouseWriteHidFile([]byte{
		2,            // Report ID 2
		byte(wheelY), // Wheel Y (signed)
		byte(wheelX), // AC Pan (signed)
	})

	u.resetUserInputTime()
	return err
}
