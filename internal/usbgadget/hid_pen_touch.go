package usbgadget

import (
	"fmt"
	"os"
)

// The pen and the touch screen live on their own HID function, not on the
// absolute mouse one.
//
// Linux decides the group of a whole hid_device (one USB interface) from any
// single usage in its report descriptor:
//
//	static void hid_scan_input_usage(struct hid_parser *parser, u32 usage)
//	{
//		if (usage == HID_DG_CONTACTID)
//			hid->group = HID_GROUP_MULTITOUCH;
//	}
//
// A touch screen collection always carries Contact Identifier, so as long as it
// shared the mouse interface, usbhid matched the entire interface to
// hid-multitouch and the Report ID 1 mouse collection stopped producing a
// pointer device. macOS and Windows build one device per top-level collection
// and were unaffected, which is why the mouse only died on Linux. Keeping the
// collections on separate functions gives each interface its own group: the
// mouse function stays HID_GROUP_GENERIC and the host keeps its cursor.

const (
	// penTouchReportLength is the report_length of the pen/touch HID function. It
	// is the size of the largest report of the function (the 10-contact touch
	// screen), and every report pads up to it: a HID host parses trailing bytes as
	// data of whatever field the descriptor declares there, so a short write would
	// be rejected or misread.
	penTouchReportLength = 64

	// Report IDs of the two collections of this function. Only one of them carries
	// the pointer at a time, chosen by the frontend mouse mode.
	penTouchReportIDPen         = 1
	penTouchReportIDTouchScreen = 2
	// penTouchReportIDTouchConfig is a feature report, so it travels over the
	// control endpoint and does not take space in an input report.
	penTouchReportIDTouchConfig = 3

	// maxTouchContacts is the number of finger collections in the touch screen
	// descriptor and the largest Contact Count the host is told to expect.
	maxTouchContacts = 10

	// Pen report flags, bit positions inside the pen collection's first byte.
	penFlagTip       = 0x01
	penFlagBarrel    = 0x02
	penFlagInRange   = 0x04
	touchFlagTip     = 0x01
	touchFlagInRange = 0x02
)

var penTouchConfig = gadgetConfigItem{
	order:      1003,
	device:     "hid.usb3",
	path:       []string{"functions", "hid.usb3"},
	configPath: []string{"hid.usb3"},
	attrs: gadgetAttributes{
		"protocol":      "2",
		"subclass":      "0",
		"report_length": "64",
	},
	reportDesc: penTouchReportDesc,
}

// TouchContact is one finger of a touch screen report. Coordinates are HID
// logical units (0-32767), not video pixels.
type TouchContact struct {
	ID      uint8
	X       int
	Y       int
	Down    bool
	InRange bool
}

// penCollectionDesc is Report ID 1 of this function: a digitizer pen. Android
// ignores the absolute axes of a mouse collection but does follow a pen, which is
// the whole reason this collection exists; In Range lets the host show a hover
// cursor before a tip press.
var penCollectionDesc = []byte{
	0x05, 0x0D, // Usage Page (Digitizers)
	0x09, 0x02, // Usage (Pen)
	0xA1, 0x01, // Collection (Application)

	0x85, 0x01, //     Report ID (1)
	0x09, 0x20, //     Usage (Stylus)
	0xA1, 0x00, //     Collection (Physical)
	0x05, 0x0D, //         Usage Page (Digitizers)
	0x09, 0x42, //         Usage (Tip Switch)
	0x09, 0x44, //         Usage (Barrel Switch)
	0x09, 0x32, //         Usage (In Range)
	0x15, 0x00, //         Logical Minimum (0)
	0x25, 0x01, //         Logical Maximum (1)
	0x75, 0x01, //         Report Size (1)
	0x95, 0x03, //         Report Count (3)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0x75, 0x05, //         Report Size (5)
	0x95, 0x01, //         Report Count (1)
	0x81, 0x03, //         Input (Cnst, Var, Abs)
	0x05, 0x01, //         Usage Page (Generic Desktop Ctrls)
	0x09, 0x30, //         Usage (X)
	0x09, 0x31, //         Usage (Y)
	0x16, 0x00, 0x00, //         Logical Minimum (0)
	0x26, 0xFF, 0x7F, //         Logical Maximum (32767)
	0x75, 0x10, //         Report Size (16)
	0x95, 0x02, //         Report Count (2)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0x05, 0x0D, //         Usage Page (Digitizers)
	0x09, 0x30, //         Usage (Tip Pressure)
	0x15, 0x00, //         Logical Minimum (0)
	0x26, 0xFF, 0x00, //         Logical Maximum (255)
	0x75, 0x08, //         Report Size (8)
	0x95, 0x01, //         Report Count (1)
	0x81, 0x02, //         Input (Data, Var, Abs)

	// Trailing padding up to penTouchReportLength: the report ID and the 6 bytes
	// above plus 57 padding bytes make 64.
	0x95, 0x39, //         Report Count (57)
	0x75, 0x08, //         Report Size (8)
	0x81, 0x03, //         Input (Cnst, Var, Abs)
	0xC0, //     End Collection
	0xC0, // End Collection
}

// touchScreenHeadDesc is the start of Report ID 2: the Contact Count byte that
// tells the host how many of the finger collections below carry a finger.
var touchScreenHeadDesc = []byte{
	0x05, 0x0D, // Usage Page (Digitizers)
	0x09, 0x04, // Usage (Touch Screen)
	0xA1, 0x01, // Collection (Application)

	0x85, 0x02, //     Report ID (2)
	0x09, 0x54, //     Usage (Contact Count)
	0x15, 0x00, //     Logical Minimum (0)
	0x25, 0x0A, //     Logical Maximum (10)
	0x75, 0x08, //     Report Size (8)
	0x95, 0x01, //     Report Count (1)
	0x81, 0x02, //     Input (Data, Var, Abs)
}

// touchFingerDesc is one finger collection of the touch screen: a status byte
// (tip switch + in range), a contact identifier, and absolute X/Y. It is emitted
// maxTouchContacts times, because a HID touch screen reports a fixed number of
// contacts and marks the unused ones empty.
var touchFingerDesc = []byte{
	0x05, 0x0D, //     Usage Page (Digitizers)
	0x09, 0x22, //     Usage (Finger)
	0xA1, 0x02, //     Collection (Logical)
	0x09, 0x42, //         Usage (Tip Switch)
	0x09, 0x32, //         Usage (In Range)
	0x15, 0x00, //         Logical Minimum (0)
	0x25, 0x01, //         Logical Maximum (1)
	0x75, 0x01, //         Report Size (1)
	0x95, 0x02, //         Report Count (2)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0x75, 0x06, //         Report Size (6)
	0x95, 0x01, //         Report Count (1)
	0x81, 0x03, //         Input (Cnst, Var, Abs)
	0x09, 0x51, //         Usage (Contact Identifier)
	0x15, 0x00, //         Logical Minimum (0)
	0x25, 0x0A, //         Logical Maximum (10)
	0x75, 0x08, //         Report Size (8)
	0x95, 0x01, //         Report Count (1)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0x05, 0x01, //         Usage Page (Generic Desktop Ctrls)
	0x09, 0x30, //         Usage (X)
	0x09, 0x31, //         Usage (Y)
	0x16, 0x00, 0x00, //         Logical Minimum (0)
	0x26, 0xFF, 0x7F, //         Logical Maximum (32767)
	0x75, 0x10, //         Report Size (16)
	0x95, 0x02, //         Report Count (2)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0xC0, //     End Collection
}

// touchScreenTailDesc closes Report ID 2 (padding up to penTouchReportLength) and
// declares the Contact Count Maximum feature report that tells the host how many
// fingers the screen supports.
var touchScreenTailDesc = []byte{
	// Padding: the report ID plus the 61 report bytes of Report ID 2 (contact count
	// and 10 fingers of 6 bytes) plus 2 padding bytes make 64.
	0x95, 0x02, //     Report Count (2)
	0x75, 0x08, //     Report Size (8)
	0x81, 0x03, //     Input (Cnst, Var, Abs)

	0x85, 0x03, //     Report ID (3)
	0x09, 0x55, //     Usage (Contact Count Maximum)
	0x15, 0x00, //     Logical Minimum (0)
	0x25, 0x0A, //     Logical Maximum (10)
	0x75, 0x08, //     Report Size (8)
	0x95, 0x01, //     Report Count (1)
	0xB1, 0x02, //     Feature (Data, Var, Abs)
	0xC0, // End Collection
}

// penTouchReportDesc is the descriptor of the pen/touch HID function: a pen and a
// touch screen in one interface, distinguished by report ID. They share a function
// because neither may share the mouse one, and both are digitizer collections the
// host keeps apart by report ID anyway.
var penTouchReportDesc = buildPenTouchReportDesc()

func buildPenTouchReportDesc() []byte {
	desc := make([]byte, 0, len(penCollectionDesc)+len(touchScreenHeadDesc)+
		maxTouchContacts*len(touchFingerDesc)+len(touchScreenTailDesc))
	desc = append(desc, penCollectionDesc...)
	desc = append(desc, touchScreenHeadDesc...)
	for i := 0; i < maxTouchContacts; i++ {
		desc = append(desc, touchFingerDesc...)
	}
	desc = append(desc, touchScreenTailDesc...)
	return desc
}

// penTouchReport frames a payload as a report of the pen/touch function: report
// ID first, then the payload, then zeros up to penTouchReportLength.
func penTouchReport(reportID byte, payload []byte) []byte {
	report := make([]byte, penTouchReportLength)
	report[0] = reportID
	copy(report[1:], payload)
	return report
}

func (u *UsbGadget) penTouchWriteHidFile(data []byte) error {
	if !u.enabledDevices.AbsoluteMouse {
		// The pen/touch function rides with the absolute mouse one: it is part of the
		// composition whenever the absolute pointer is, so switching the pointer off
		// removes this node as well.
		return ErrHIDDeviceNotConfigured
	}
	if u.penTouchHidFile == nil {
		var err error
		hidNodePath := u.HidgNodePath("pen_touch")
		u.penTouchHidFile, err = os.OpenFile(hidNodePath, os.O_RDWR, 0666)
		if err != nil {
			if isMissingHidNodeError(err) {
				if u.hidNodeGenuinelyMissing() {
					u.log.Error().
						Str("device", hidNodePath).
						Str("device_name", "pen_touch").
						Err(err).
						Msg("HID device file missing, gadget may need reinitialization")
					if u.onHidDeviceMissing != nil {
						(*u.onHidDeviceMissing)("pen_touch", err)
					}
				} else {
					u.log.Debug().
						Str("device", hidNodePath).
						Err(err).
						Msg("pen/touch hidg node not present yet; host has not finished enumeration")
				}
			}
			return fmt.Errorf("failed to open %s: %w", hidNodePath, err)
		}
	}

	_, err := u.writeWithTimeout(u.penTouchHidFile, data)
	if err != nil {
		u.logWithSuppression("penTouchWriteHidFile", 100, u.log, err, "failed to write to pen/touch hidg node")
		u.penTouchHidFile.Close()
		u.penTouchHidFile = nil
		return err
	}
	u.resetLogSuppressionCounter("penTouchWriteHidFile")
	return nil
}

// writePenTouchReport writes one report of the pen/touch function and marks the
// gadget as recently used.
func (u *UsbGadget) writePenTouchReport(report []byte) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.penTouchLock.Lock()
	defer u.penTouchLock.Unlock()

	if err := u.penTouchWriteHidFile(report); err != nil {
		return err
	}
	u.resetUserInputTime()
	return nil
}

// PenReport moves the pen collection's cursor and sets its switches. inRange
// reports the pen as hovering above the surface, which is what makes a host show
// a cursor without a button press. tip is a left click, barrel a right click, and
// pressure is the 0-255 tip pressure some hosts use to decide whether a stroke is
// real.
func (u *UsbGadget) PenReport(x, y int, tip, barrel, inRange bool, pressure uint8) error {
	var flags uint8
	if tip {
		flags |= penFlagTip
	}
	if barrel {
		flags |= penFlagBarrel
	}
	if inRange {
		flags |= penFlagInRange
	}

	return u.writePenTouchReport(penTouchReport(penTouchReportIDPen, []byte{
		flags,
		uint8(x),      // X Low Byte
		uint8(x >> 8), // X High Byte
		uint8(y),      // Y Low Byte
		uint8(y >> 8), // Y High Byte
		pressure,
	}))
}

// TouchReport reports the current set of fingers of the touch screen collection.
//
// A contact keeps the slot it was given in ID. Hosts track touch state per slot,
// so a finger that moves to another slot between two reports looks like a new
// contact while its old slot stays pressed: with two fingers on the surface,
// releasing one used to shift the other into its slot and strand the first one
// down forever. Slots therefore stay put for the whole lifetime of a contact.
//
// InRange is reported separately from Down so a lift can be announced while the
// slot is still in range. Dropping both flags in the same report is what leaves
// some hosts with a contact that never sees a release; the client sends one
// down=false, inRange=true report first and stops mentioning the slot afterwards.
func (u *UsbGadget) TouchReport(contacts []TouchContact) error {
	payload := make([]byte, 1+maxTouchContacts*6)

	count := 0
	for _, contact := range contacts {
		slot := int(contact.ID)
		if slot >= maxTouchContacts {
			continue
		}

		if slot+1 > count {
			count = slot + 1
		}

		flags := uint8(0)
		if contact.Down {
			flags |= touchFlagTip
		}
		if contact.Down || contact.InRange {
			flags |= touchFlagInRange
		}

		offset := 1 + slot*6
		payload[offset] = flags
		payload[offset+1] = contact.ID
		payload[offset+2] = uint8(contact.X)
		payload[offset+3] = uint8(contact.X >> 8)
		payload[offset+4] = uint8(contact.Y)
		payload[offset+5] = uint8(contact.Y >> 8)
	}

	payload[0] = uint8(count)

	return u.writePenTouchReport(penTouchReport(penTouchReportIDTouchScreen, payload))
}

// HasPenTouch reports whether the pen/touch HID function is part of the gadget.
// It follows the absolute mouse function, which is the one that carries the
// pointer these collections drive.
func (u *UsbGadget) HasPenTouch() bool {
	return u.enabledDevices.AbsoluteMouse
}
