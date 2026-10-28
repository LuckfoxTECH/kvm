package usbgadget

const dwc3Path = "/sys/bus/platform/drivers/dwc3"
const udcPath = "/sys/kernel/config/usb_gadget/kvm/UDC"

// USB state strings as reported by the kernel through sysfs.
const (
	USBStateConfigured  = "configured"
	USBStateDefault     = "default"
	USBStateNotAttached = "not attached"
	USBStateUnknown     = "unknown"
)

const (
	// hidKeyBufferSize is the number of simultaneous non-modifier keys a boot
	// keyboard report can carry.
	hidKeyBufferSize = 6

	// hidErrorRollOver is the HID error-roll-over usage, reported when more keys
	// are held than the buffer can represent.
	hidErrorRollOver = 0x01
)

// HID keyboard usage IDs (HID Usage Tables, Keyboard/Keypad page 0x07).
const (
	LeftControl  = 0xE0
	LeftShift    = 0xE1
	LeftAlt      = 0xE2
	LeftSuper    = 0xE3 // Left GUI (Windows key / Command key)
	RightControl = 0xE4
	RightShift   = 0xE5
	RightAlt     = 0xE6
	RightSuper   = 0xE7 // Right GUI (Windows key / Command key)
)

// Modifier byte bits. Left and right sides occupy distinct bits, which is what
// makes per-side modifier tracking possible at all.
const (
	ModifierMaskLeftControl  = 0x01
	ModifierMaskRightControl = 0x10
	ModifierMaskLeftShift    = 0x02
	ModifierMaskRightShift   = 0x20
	ModifierMaskLeftAlt      = 0x04
	ModifierMaskRightAlt     = 0x40
	ModifierMaskLeftSuper    = 0x08
	ModifierMaskRightSuper   = 0x80
)

// KeyCodeToMaskMap maps a modifier key usage ID to its bit in the modifier byte.
var KeyCodeToMaskMap = map[byte]byte{
	LeftControl:  ModifierMaskLeftControl,
	LeftShift:    ModifierMaskLeftShift,
	LeftAlt:      ModifierMaskLeftAlt,
	LeftSuper:    ModifierMaskLeftSuper,
	RightControl: ModifierMaskRightControl,
	RightShift:   ModifierMaskRightShift,
	RightAlt:     ModifierMaskRightAlt,
	RightSuper:   ModifierMaskRightSuper,
}
