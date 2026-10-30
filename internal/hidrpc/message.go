package hidrpc

import (
	"encoding/binary"
	"fmt"
)

// Message ..
type Message struct {
	t MessageType
	d []byte
}

// Marshal marshals the message to a byte array.
func (m *Message) Marshal() ([]byte, error) {
	return Marshal(m)
}

func (m *Message) Type() MessageType {
	return m.t
}

// Version returns the protocol version carried by a handshake message.
func (m *Message) Version() (byte, error) {
	if m.t != TypeHandshake {
		return 0, fmt.Errorf("invalid message type: %d", m.t)
	}
	if len(m.d) < 1 {
		return 0, fmt.Errorf("invalid handshake payload length: %d", len(m.d))
	}

	return m.d[0], nil
}

func (m *Message) String() string {
	switch m.t {
	case TypeHandshake:
		return "Handshake"
	case TypeKeypressReport:
		if len(m.d) < 2 {
			return fmt.Sprintf("KeypressReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("KeypressReport{Key: %d, Press: %v}", m.d[0], m.d[1] == uint8(1))
	case TypeKeyboardReport:
		if len(m.d) < 2 {
			return fmt.Sprintf("KeyboardReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("KeyboardReport{Modifier: %d, Keys: %v}", m.d[0], m.d[1:])
	case TypePointerReport:
		if len(m.d) < 9 {
			return fmt.Sprintf("PointerReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("PointerReport{X: %d, Y: %d, Button: %d}", m.d[0:4], m.d[4:8], m.d[8])
	case TypeMouseReport:
		if len(m.d) < 3 {
			return fmt.Sprintf("MouseReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("MouseReport{DX: %d, DY: %d, Button: %d}", m.d[0], m.d[1], m.d[2])
	case TypePenReport:
		if len(m.d) < 10 {
			return fmt.Sprintf("PenReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("PenReport{X: %d, Y: %d, Flags: %d, Pressure: %d}",
			toInt(m.d[0:4]), toInt(m.d[4:8]), m.d[8], m.d[9])
	case TypeTouchReport:
		if len(m.d) < 1 {
			return fmt.Sprintf("TouchReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("TouchReport{ContactCount: %d}", m.d[0])
	case TypeKeypressKeepAliveReport:
		return "KeypressKeepAliveReport"
	case TypeKeyboardMacroReport:
		if len(m.d) < 5 {
			return fmt.Sprintf("KeyboardMacroReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("KeyboardMacroReport{IsPaste: %v, Length: %d}", m.d[0] == uint8(1), binary.BigEndian.Uint32(m.d[1:5]))
	default:
		return fmt.Sprintf("Unknown{Type: %d, Data: %v}", m.t, m.d)
	}
}

// KeypressReport ..
type KeypressReport struct {
	Key   byte
	Press bool
}

// KeypressReport returns the keypress report from the message.
func (m *Message) KeypressReport() (KeypressReport, error) {
	if m.t != TypeKeypressReport {
		return KeypressReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}
	if len(m.d) != 2 {
		return KeypressReport{}, fmt.Errorf("invalid keypress payload length: %d", len(m.d))
	}

	return KeypressReport{
		Key:   m.d[0],
		Press: m.d[1] == uint8(1),
	}, nil
}

// KeyboardReport ..
type KeyboardReport struct {
	Modifier byte
	Keys     []byte
}

// KeyboardReport returns the keyboard report from the message.
func (m *Message) KeyboardReport() (KeyboardReport, error) {
	if m.t != TypeKeyboardReport {
		return KeyboardReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}
	if len(m.d) < 1 || len(m.d) > 1+HidKeyBufferSize {
		return KeyboardReport{}, fmt.Errorf("invalid keyboard payload length: %d", len(m.d))
	}

	return KeyboardReport{
		Modifier: m.d[0],
		Keys:     m.d[1:],
	}, nil
}

// Macro ..
type KeyboardMacroStep struct {
	Modifier byte   // 1 byte
	Keys     []byte // 6 bytes: hidKeyBufferSize
	Delay    uint16 // 2 bytes
}
type KeyboardMacroReport struct {
	IsPaste   bool
	StepCount uint32
	Steps     []KeyboardMacroStep
}

// HidKeyBufferSize is the size of the keys buffer in the keyboard report.
const HidKeyBufferSize = 6

// KeyboardMacroReport returns the keyboard macro report from the message.
func (m *Message) KeyboardMacroReport() (KeyboardMacroReport, error) {
	if m.t != TypeKeyboardMacroReport {
		return KeyboardMacroReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}
	if len(m.d) < 5 {
		return KeyboardMacroReport{}, fmt.Errorf("invalid macro payload length: %d", len(m.d))
	}

	isPaste := m.d[0] == uint8(1)
	stepCount := binary.BigEndian.Uint32(m.d[1:5])

	// Derive the count from the payload to avoid count multiplication overflowing
	// an int on the device's 32-bit architecture.
	if (len(m.d)-5)%9 != 0 || uint64(stepCount) != uint64((len(m.d)-5)/9) {
		return KeyboardMacroReport{}, fmt.Errorf("invalid macro length: %d for %d steps", len(m.d), stepCount)
	}

	steps := make([]KeyboardMacroStep, 0, int(stepCount))
	offset := 5
	for i := 0; i < int(stepCount); i++ {
		steps = append(steps, KeyboardMacroStep{
			Modifier: m.d[offset],
			Keys:     m.d[offset+1 : offset+7],
			Delay:    binary.BigEndian.Uint16(m.d[offset+7 : offset+9]),
		})

		offset += 1 + HidKeyBufferSize + 2
	}

	return KeyboardMacroReport{
		IsPaste:   isPaste,
		Steps:     steps,
		StepCount: stepCount,
	}, nil
}

// PointerReport ..
type PointerReport struct {
	X      int
	Y      int
	Button uint8
}

func toInt(b []byte) int {
	return int(b[0])<<24 + int(b[1])<<16 + int(b[2])<<8 + int(b[3])<<0
}

// PointerReport returns the point report from the message.
func (m *Message) PointerReport() (PointerReport, error) {
	if m.t != TypePointerReport {
		return PointerReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}

	if len(m.d) != 9 {
		return PointerReport{}, fmt.Errorf("invalid message length: %d", len(m.d))
	}

	return PointerReport{
		X:      toInt(m.d[0:4]),
		Y:      toInt(m.d[4:8]),
		Button: uint8(m.d[8]),
	}, nil
}

// MouseReport ..
type MouseReport struct {
	DX     int8
	DY     int8
	Button uint8
}

// MouseReport returns the mouse report from the message.
func (m *Message) MouseReport() (MouseReport, error) {
	if m.t != TypeMouseReport {
		return MouseReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}
	if len(m.d) != 3 {
		return MouseReport{}, fmt.Errorf("invalid mouse payload length: %d", len(m.d))
	}

	return MouseReport{
		DX:     int8(m.d[0]),
		DY:     int8(m.d[1]),
		Button: uint8(m.d[2]),
	}, nil
}

// Pen report flag bits, matching the pen collection descriptor in
// internal/usbgadget/hid_mouse_absolute.go.
const (
	PenFlagTip     uint8 = 0x01
	PenFlagBarrel  uint8 = 0x02
	PenFlagInRange uint8 = 0x04

	TouchFlagTip     uint8 = 0x01
	TouchFlagInRange uint8 = 0x02
)

// MaxTouchContacts is the number of finger collections the touch screen reports.
const MaxTouchContacts = 10

// PenReport is one stylus report: an absolute position plus its switches.
type PenReport struct {
	X        int
	Y        int
	Flags    uint8
	Pressure uint8
}

func (r PenReport) IsTip() bool     { return r.Flags&PenFlagTip != 0 }
func (r PenReport) IsBarrel() bool  { return r.Flags&PenFlagBarrel != 0 }
func (r PenReport) IsInRange() bool { return r.Flags&PenFlagInRange != 0 }

// PenReport returns the pen report from the message. The payload is the position
// as two big-endian int32s, the flag byte and the tip pressure.
func (m *Message) PenReport() (PenReport, error) {
	if m.t != TypePenReport {
		return PenReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}
	if len(m.d) != 10 {
		return PenReport{}, fmt.Errorf("invalid pen payload length: %d", len(m.d))
	}

	return PenReport{
		X:        toInt(m.d[0:4]),
		Y:        toInt(m.d[4:8]),
		Flags:    m.d[8],
		Pressure: m.d[9],
	}, nil
}

// TouchContact is one finger of a touch screen report.
type TouchContact struct {
	ID    uint8
	X     int
	Y     int
	Flags uint8
}

func (c TouchContact) IsDown() bool { return c.Flags&TouchFlagTip != 0 }

// IsInRange reports whether the host should keep following the contact's position
// even though the tip is up; a lift is announced as tip=0, inRange=1.
func (c TouchContact) IsInRange() bool { return c.Flags&TouchFlagInRange != 0 }

// TouchReport is the set of fingers currently on the surface.
type TouchReport struct {
	Contacts []TouchContact
}

// TouchReport returns the touch report from the message. The payload is the
// contact count followed by that many contacts, each a contact ID, the position
// as two big-endian int32s and the flag byte.
func (m *Message) TouchReport() (TouchReport, error) {
	if m.t != TypeTouchReport {
		return TouchReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}
	if len(m.d) < 1 {
		return TouchReport{}, fmt.Errorf("invalid touch payload length: %d", len(m.d))
	}

	count := int(m.d[0])
	if count > MaxTouchContacts {
		return TouchReport{}, fmt.Errorf("too many touch contacts: %d", count)
	}
	if len(m.d) != 1+count*10 {
		return TouchReport{}, fmt.Errorf("invalid touch payload length: %d for %d contacts", len(m.d), count)
	}

	contacts := make([]TouchContact, 0, count)
	for i := 0; i < count; i++ {
		offset := 1 + i*10
		contacts = append(contacts, TouchContact{
			ID:    m.d[offset],
			X:     toInt(m.d[offset+1 : offset+5]),
			Y:     toInt(m.d[offset+5 : offset+9]),
			Flags: m.d[offset+9],
		})
	}

	return TouchReport{Contacts: contacts}, nil
}

type KeyboardMacroState struct {
	State   bool
	IsPaste bool
}

// KeyboardMacroState returns the keyboard macro state report from the message.
func (m *Message) KeyboardMacroState() (KeyboardMacroState, error) {
	if m.t != TypeKeyboardMacroState {
		return KeyboardMacroState{}, fmt.Errorf("invalid message type: %d", m.t)
	}
	if len(m.d) != 2 {
		return KeyboardMacroState{}, fmt.Errorf("invalid macro state payload length: %d", len(m.d))
	}

	return KeyboardMacroState{
		State:   m.d[0] == uint8(1),
		IsPaste: m.d[1] == uint8(1),
	}, nil
}
