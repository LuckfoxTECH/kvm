package kvm

import (
	"fmt"
	"strings"
)

// This file is the single place where the HTTP surfaces (the local CLI API and
// the MCP server) touch the HID gadget. Both surfaces call these helpers, so a
// fix or a safety check lands in both of them at once. Only the argument
// semantics that genuinely differ between the two stay in the handlers.

const (
	// hidAbsMax is the logical maximum of the absolute pointer axes.
	hidAbsMax = 32767

	mouseModeAbsolute = "absolute"
	mouseModeRelative = "relative"

	// maxTypeTextLength caps a single type_text call so one request cannot
	// keep the remote keyboard busy for minutes.
	maxTypeTextLength = 1000
)

// === Mouse ===

// absMouse moves the absolute pointer and sets the given buttons.
func absMouse(x, y int, buttons uint8) error {
	_, err := callRPCHandler(rpcHandlers["absMouseReport"], map[string]interface{}{
		"x": x, "y": y, "buttons": buttons,
	})
	return err
}

// relMouse moves the relative pointer and sets the given buttons.
func relMouse(dx, dy int8, buttons uint8) error {
	_, err := callRPCHandler(rpcHandlers["relMouseReport"], map[string]interface{}{
		"dx": dx, "dy": dy, "buttons": buttons,
	})
	return err
}

// wheelScroll sends a single wheel report for the given mouse mode.
func wheelScroll(steps int8, mouseMode string) error {
	if mouseMode != mouseModeRelative {
		mouseMode = mouseModeAbsolute
	}
	_, err := callRPCHandler(rpcHandlers["wheelReport"], map[string]interface{}{
		"wheelY": steps, "mouseMode": mouseMode,
	})
	return err
}

// mouseButtonBit maps a button name to its HID button bit.
func mouseButtonBit(button string) (uint8, error) {
	switch button {
	case "left":
		return 1, nil
	case "right":
		return 2, nil
	case "middle":
		return 4, nil
	}
	return 0, fmt.Errorf("unknown button: %s", button)
}

// clickMouse clicks where the absolute pointer already is. Absolute HID reports
// always carry a position, so sending (0, 0) would teleport the remote cursor
// into the top-left corner instead of clicking in place.
func clickMouse(button string, clicks int) error {
	bit, err := mouseButtonBit(button)
	if err != nil {
		return err
	}
	if clicks < 1 {
		clicks = 1
	}
	if clicks > 5 {
		return fmt.Errorf("clicks must be between 1 and 5")
	}

	x, y := gadget.GetAbsMousePosition()
	for i := 0; i < clicks; i++ {
		if err := absMouse(x, y, bit); err != nil {
			return err
		}
		if err := absMouse(x, y, 0); err != nil {
			return err
		}
	}
	return nil
}

// moveMouseToPixel moves the absolute pointer to a pixel coordinate of the
// video input and returns the coordinate after clamping to the visible area.
func moveMouseToPixel(x, y int) (px, py int, err error) {
	state, err := rpcGetVideoState()
	if err != nil {
		return 0, 0, err
	}
	if state.Width < 2 || state.Height < 2 {
		return 0, 0, fmt.Errorf("video resolution unknown (%dx%d), cannot map pixel coordinates", state.Width, state.Height)
	}

	maxX, maxY := state.Width-1, state.Height-1
	px = clampInt(x, 0, maxX)
	py = clampInt(y, 0, maxY)

	// scale pixels to HID logical units, rounded instead of truncated
	absX := (px*hidAbsMax + maxX/2) / maxX
	absY := (py*hidAbsMax + maxY/2) / maxY
	if err := absMouse(absX, absY, 0); err != nil {
		return 0, 0, err
	}
	return px, py, nil
}

// === Keyboard ===

// keyboardReport sends a keyboard report without releasing it, so a caller can
// hold keys down across several requests.
func keyboardReport(modifier uint8, keys []uint8) error {
	_, err := callRPCHandler(rpcHandlers["keyboardReport"], map[string]interface{}{
		"modifier": modifier, "keys": keys,
	})
	return err
}

// tapKeys presses the given keys and releases everything again.
func tapKeys(modifier uint8, keys []uint8) error {
	if err := keyboardReport(modifier, keys); err != nil {
		return err
	}
	return releaseKeys()
}

// releaseKeys releases every pressed key.
func releaseKeys() error {
	return keyboardReport(0, []uint8{})
}

var modifierBits = map[string]uint8{
	"ctrl":    0x01,
	"control": 0x01,
	"shift":   0x02,
	"alt":     0x04,
	"meta":    0x08,
	"win":     0x08,
	"cmd":     0x08,
}

// parseCombo splits key names into modifier bits and HID key codes.
func parseCombo(keyNames []string) (modifier uint8, keys []uint8, err error) {
	for _, name := range keyNames {
		if bit, ok := modifierBits[strings.ToLower(name)]; ok {
			modifier |= bit
			continue
		}
		code, ok := keyNameToCode[name]
		if !ok {
			return 0, nil, fmt.Errorf("unknown key: %s", name)
		}
		keys = append(keys, code)
	}
	return modifier, keys, nil
}

// typeASCIIText types the text one character at a time. The whole string is
// validated first, so a rejected character never leaves half of the text
// already typed on the remote host.
func typeASCIIText(text string) error {
	runes := []rune(text)
	if len(runes) > maxTypeTextLength {
		return fmt.Errorf("text is longer than %d characters", maxTypeTextLength)
	}

	codes := make([]uint8, 0, len(runes))
	modifiers := make([]uint8, 0, len(runes))
	for i, char := range runes {
		if char > 0x7F {
			return fmt.Errorf("unsupported non-ASCII character %q at index %d, only ASCII text can be typed", char, i)
		}
		code, modifier, ok := charToKeyCode(uint8(char))
		if !ok {
			continue
		}
		codes = append(codes, code)
		modifiers = append(modifiers, modifier)
	}

	for i := range codes {
		if err := tapKeys(modifiers[i], []uint8{codes[i]}); err != nil {
			return err
		}
	}
	return nil
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
