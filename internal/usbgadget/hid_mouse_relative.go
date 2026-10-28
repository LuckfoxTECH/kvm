package usbgadget

import (
	"fmt"
	"os"
)

var relativeMouseConfig = gadgetConfigItem{
	order:      1002,
	device:     "hid.usb2",
	path:       []string{"functions", "hid.usb2"},
	configPath: []string{"hid.usb2"},
	attrs: gadgetAttributes{
		"protocol":      "2",
		"subclass":      "1",
		"report_length": "5",
	},
	reportDesc: relativeMouseCombinedReportDesc,
}

// from: https://github.com/NicoHood/HID/blob/b16be57caef4295c6cd382a7e4c64db5073647f7/src/SingleReport/BootMouse.cpp#L26
var relativeMouseCombinedReportDesc = []byte{
	0x05, 0x01, // USAGE_PAGE (Generic Desktop)	  54
	0x09, 0x02, // USAGE (Mouse)
	0xa1, 0x01, // COLLECTION (Application)

	// Pointer and Physical are required by Apple Recovery
	0x09, 0x01, // USAGE (Pointer)
	0xa1, 0x00, // COLLECTION (Physical)

	// 8 Buttons
	0x05, 0x09, // USAGE_PAGE (Button)
	0x19, 0x01, // USAGE_MINIMUM (Button 1)
	0x29, 0x08, // USAGE_MAXIMUM (Button 8)
	0x15, 0x00, // LOGICAL_MINIMUM (0)
	0x25, 0x01, // LOGICAL_MAXIMUM (1)
	0x95, 0x08, // REPORT_COUNT (8)
	0x75, 0x01, // REPORT_SIZE (1)
	0x81, 0x02, // INPUT (Data,Var,Abs)

	// X, Y, Wheel
	0x05, 0x01, // USAGE_PAGE (Generic Desktop)
	0x09, 0x30, // USAGE (X)
	0x09, 0x31, // USAGE (Y)
	0x09, 0x38, // USAGE (Wheel)
	0x15, 0x81, // LOGICAL_MINIMUM (-127)
	0x25, 0x7f, // LOGICAL_MAXIMUM (127)
	0x75, 0x08, // REPORT_SIZE (8)
	0x95, 0x03, // REPORT_COUNT (3)
	0x81, 0x06, // INPUT (Data,Var,Rel)

	// Horizontal Scroll
	0x05, 0x0C, // USAGE_PAGE (Consumer)
	0x0A, 0x38, 0x02, // USAGE (AC Pan)
	0x15, 0x81, // LOGICAL_MINIMUM (-127)
	0x25, 0x7f, // LOGICAL_MAXIMUM (127)
	0x75, 0x08, // REPORT_SIZE (8)
	0x95, 0x01, // REPORT_COUNT (1)
	0x81, 0x06, // INPUT (Data,Var,Rel)

	// End
	0xc0, //       End Collection (Physical)
	0xc0, //       End Collection
}

func (u *UsbGadget) relMouseWriteHidFile(data []byte) error {
	if !u.enabledDevices.RelativeMouse {
		// Switched off, so /dev/hidg2 is absent by design rather than missing.
		return ErrHIDDeviceNotConfigured
	}
	if u.relMouseHidFile == nil {
		var err error
		hidNodePath := u.HidgNodePath("relative_mouse")
		u.relMouseHidFile, err = os.OpenFile(hidNodePath, os.O_RDWR, 0666)
		if err != nil {

			if isMissingHidNodeError(err) {
				if u.hidNodeGenuinelyMissing() {
					u.log.Error().
						Str("device", hidNodePath).
						Str("device_name", "relative_mouse").
						Err(err).
						Msg("HID device file missing, gadget may need reinitialization")

					if u.onHidDeviceMissing != nil {
						(*u.onHidDeviceMissing)("relative_mouse", err)
					}
				} else {
					u.log.Debug().
						Str("device", hidNodePath).
						Err(err).
						Msg("hidg2 not present yet; host has not finished enumeration")
				}
			}
			return fmt.Errorf("failed to open %s: %w", hidNodePath, err)
		}
	}

	_, err := u.writeWithTimeout(u.relMouseHidFile, data)
	if err != nil {
		u.logWithSuppression("relMouseWriteHidFile", 100, u.log, err, "failed to write to hidg2")
		u.relMouseHidFile.Close()
		u.relMouseHidFile = nil
		return err
	}
	u.resetLogSuppressionCounter("relMouseWriteHidFile")
	return nil
}

func (u *UsbGadget) RelMouseReport(mx, my int8, buttons uint8, wheel int8) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.relMouseLock.Lock()
	defer u.relMouseLock.Unlock()

	err := u.relMouseWriteHidFile([]byte{
		buttons,      // Buttons
		uint8(mx),    // X
		uint8(my),    // Y
		uint8(wheel), // Wheel
		0,            // AC Pan
	})
	if err != nil {
		return err
	}

	u.resetUserInputTime()
	return nil
}

// RelMouseWheelReport sends a wheel-only relative mouse report. wheelY scrolls
// vertically (negative is up) and wheelX scrolls horizontally.
func (u *UsbGadget) RelMouseWheelReport(wheelY, wheelX int8) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.relMouseLock.Lock()
	defer u.relMouseLock.Unlock()

	if wheelY == 0 && wheelX == 0 {
		return nil
	}

	err := u.relMouseWriteHidFile([]byte{
		0,             // Buttons
		0,             // X
		0,             // Y
		uint8(wheelY), // Wheel
		uint8(wheelX), // AC Pan
	})
	if err != nil {
		return err
	}

	u.resetUserInputTime()
	return nil
}
