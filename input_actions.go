package kvm

import (
	"fmt"
	"strings"
	"time"
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

	// reportGap separates a press report from its release report. The gadget
	// coalesces two reports that arrive inside the same USB frame, and the remote
	// host then sees only one of them: the keystroke disappears or the key stays
	// held down and repeats forever. Waiting longer than one frame (1 ms) costs
	// nothing noticeable but removes both failure modes.
	reportGap = 15 * time.Millisecond
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

// wheelScroll sends a single wheel report. The device picks the report channel
// from the gadget's capabilities (see rpcWheelReport in usb.go), so mouseMode is
// advisory only. It stays in the signature so the MCP and HTTP contracts do not
// change, but it must not be forwarded: wheelReport accepts wheelY and wheelX
// only, and passing an unknown parameter fails the call.
func wheelScroll(steps int8, wheelX int8, _ string) error {
	_, err := callRPCHandler(rpcHandlers["wheelReport"], map[string]interface{}{
		"wheelY": steps, "wheelX": wheelX,
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
		// a release in the same USB frame as the press is coalesced away, the
		// click is then lost or the button stays pressed
		time.Sleep(reportGap)
		if err := absMouse(x, y, 0); err != nil {
			return err
		}
		if i < clicks-1 {
			time.Sleep(reportGap)
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

// maxRelativeStep is the largest delta one HID relative report can carry.
const maxRelativeStep = 127

// maxRelativeMove bounds a single call, so the loop below cannot be turned into
// a long spin by an absurd argument.
const maxRelativeMove = 2047

// moveMouseRelative moves the pointer by a signed pixel delta, split into as
// many HID reports as the delta needs: the report field is signed 8-bit, so a
// 1000 px move is eight reports, not a silent clamp to 127. It returns the delta
// that was actually applied.
func moveMouseRelative(dx, dy int) (int, int, error) {
	appliedX := clampInt(dx, -maxRelativeMove, maxRelativeMove)
	appliedY := clampInt(dy, -maxRelativeMove, maxRelativeMove)

	remainingX, remainingY := appliedX, appliedY
	for remainingX != 0 || remainingY != 0 {
		stepX := clampInt(remainingX, -maxRelativeStep, maxRelativeStep)
		stepY := clampInt(remainingY, -maxRelativeStep, maxRelativeStep)
		if err := relMouse(int8(stepX), int8(stepY), 0); err != nil {
			return 0, 0, err
		}
		remainingX -= stepX
		remainingY -= stepY
		if remainingX != 0 || remainingY != 0 {
			// the remote USB HID stack drops reports sent back to back
			time.Sleep(2 * time.Millisecond)
		}
	}
	return appliedX, appliedY, nil
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

// tapKeys presses the given keys and releases everything again. The two reports
// are separated by reportGap: writing them back to back lets the gadget merge
// them into one, which drops the keystroke or leaves the key held down.
func tapKeys(modifier uint8, keys []uint8) error {
	if err := keyboardReport(modifier, keys); err != nil {
		return err
	}
	time.Sleep(reportGap)
	return releaseKeys()
}

// releaseKeys releases every pressed key.
func releaseKeys() error {
	return keyboardReport(0, []uint8{})
}

var modifierBits = map[string]uint8{
	// Bare names keep working and mean the left side, so existing callers and
	// trained models keep behaving the same.
	"ctrl":    0x01,
	"control": 0x01,
	"shift":   0x02,
	"alt":     0x04,
	"meta":    0x08,
	"win":     0x08,
	"cmd":     0x08,

	// Per-side names. The modifier byte has a distinct bit per side, so a right
	// shift or a right Alt (AltGr on JIS layouts) cannot be expressed by the bare
	// names at all, no matter how the caller spells them.
	"ctrlleft":     0x01,
	"controlleft":  0x01,
	"shiftleft":    0x02,
	"altleft":      0x04,
	"metaleft":     0x08,
	"winleft":      0x08,
	"cmdleft":      0x08,
	"ctrlright":    0x10,
	"controlright": 0x10,
	"shiftright":   0x20,
	"altright":     0x40, // AltGr on JIS and most European layouts
	"altgr":        0x40,
	"metaright":    0x80,
	"winright":     0x80,
	"cmdright":     0x80,
}

// parseCombo splits key names into modifier bits and HID key codes.
func parseCombo(keyNames []string) (modifier uint8, keys []uint8, err error) {
	for _, name := range keyNames {
		if bit, ok := keyModifier(name); ok {
			modifier |= bit
			continue
		}
		code, shift, ok := lookupKeyCodeWithShift(name)
		if !ok {
			return 0, nil, fmt.Errorf("unknown key: %s", name)
		}
		// A character that sits on a shifted key brings its own Shift bit: ctrl and
		// "+" is Ctrl+Shift+= on a US layout, which is what zooms a browser in.
		modifier |= shift
		keys = append(keys, code)
	}
	return modifier, keys, nil
}

// lowercaseKeyCodes mirrors keyNameToCode with lower case names, so a caller
// that writes "enter" or "pageup" is understood instead of punished. Models
// spell key names however the vendor they were trained on spelled them.
var lowercaseKeyCodes = func() map[string]uint8 {
	codes := make(map[string]uint8, len(keyNameToCode))
	for name, code := range keyNameToCode {
		lower := strings.ToLower(name)
		// single letters keep their meaning; the shift modifier decides the case
		if _, taken := codes[lower]; !taken {
			codes[lower] = code
		}
	}
	return codes
}()

// lookupKeyCode resolves a key name to a bare HID code, case-insensitively. A
// single punctuation character means the key it sits on, so "-" is Minus. A
// character that needs Shift is rejected here instead of returning the
// unshifted key, because this result carries no modifier: use parseCombo, which
// folds the Shift bit in.
func lookupKeyCode(name string) (uint8, bool) {
	code, shift, ok := lookupKeyCodeWithShift(name)
	if !ok || shift != 0 {
		return 0, false
	}
	return code, true
}

// lookupKeyCodeWithShift also reports the Shift bit a character needs. Named
// keys (Enter, Minus, F1) never need one; punctuation falls through to
// charToKeyCode, the same table type_text uses.
func lookupKeyCodeWithShift(name string) (keyCode uint8, shift uint8, ok bool) {
	if code, found := keyNameToCode[name]; found {
		return code, 0, true
	}
	if code, found := lowercaseKeyCodes[strings.ToLower(name)]; found {
		return code, 0, true
	}
	if runes := []rune(name); len(runes) == 1 && runes[0] <= 0x7F {
		return charToKeyCode(uint8(runes[0]))
	}
	return 0, 0, false
}

// keyModifier resolves a modifier name to its report bit.
func keyModifier(name string) (uint8, bool) {
	bit, ok := modifierBits[strings.ToLower(name)]
	return bit, ok
}

// typeASCIIText types the text one character at a time. The whole string is
// validated first, so a rejected character never leaves half of the text
// already typed on the remote host. A non-zero delay is inserted between two
// keystrokes because the remote USB HID stack drops keys when reports are sent
// back to back.
func typeASCIIText(text string, delay time.Duration) error {
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
		// tapKeys already waited reportGap after the press, so only the rest of the
		// requested rhythm has to be added
		if gap := delay - reportGap; gap > 0 && i < len(codes)-1 {
			time.Sleep(gap)
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
