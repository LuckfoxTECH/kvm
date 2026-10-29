package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// Tool handlers. All HID calls go through deps.Input, which the daemon wires to
// the very same primitives the local CLI API uses, so a fix on one surface can
// never miss the other.

// toolError reports a failure as a tool result instead of a transport error, so
// the model can read the reason and correct its call.
func toolError(format string, args ...interface{}) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(fmt.Sprintf(format, args...)), nil
}

// toolErrorResult is toolError for the helpers that only return a result, not a
// transport error.
func toolErrorResult(format string, args ...interface{}) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf(format, args...))
}

func argFloat(args map[string]interface{}, name string) (float64, bool) {
	value, ok := args[name].(float64)
	return value, ok
}

func argString(args map[string]interface{}, name string) string {
	value, _ := args[name].(string)
	return value
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

// withInput runs fn while holding the input lease, so a keyboard write is never
// interleaved with another writer's keystrokes.
func withInput(tool string, hold time.Duration, fn func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	release, err := AcquireInput(OwnerTool(tool), hold)
	if err != nil {
		return toolError("%v", err)
	}
	defer release()
	return fn()
}

func handleMouseMove(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	mode := strings.ToLower(argString(args, "mode"))
	if mode == "" {
		mode = "absolute"
	}

	switch mode {
	case "absolute":
		x, okX := argFloat(args, "x")
		y, okY := argFloat(args, "y")
		if !okX || !okY {
			return toolError("mode=absolute requires numeric x and y")
		}
		px, py, err := deps.Input.MoveToPixel(int(x), int(y))
		if err != nil {
			return toolError("%v", err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Mouse moved to pixel (%d, %d)", px, py)), nil

	case "relative":
		dx, okX := argFloat(args, "dx")
		dy, okY := argFloat(args, "dy")
		if !okX || !okY {
			return toolError("mode=relative requires numeric dx and dy")
		}
		appliedX, appliedY, err := deps.Input.MoveRelative(int(dx), int(dy))
		if err != nil {
			return toolError("%v", err)
		}
		if appliedX != int(dx) || appliedY != int(dy) {
			return mcp.NewToolResultText(fmt.Sprintf(
				"Mouse moved by (%d, %d), limited from (%d, %d); repeat the call for larger moves",
				appliedX, appliedY, int(dx), int(dy))), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Mouse moved by (%d, %d)", appliedX, appliedY)), nil
	}

	return toolError("unknown mode %q, use absolute or relative", mode)
}

func handleMouseClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	button := argString(args, "button")

	clicks := 1
	if value, ok := argFloat(args, "clicks"); ok {
		clicks = int(value)
	}
	if clicks < 1 || clicks > maxClickCount {
		return toolError("clicks must be between 1 and %d", maxClickCount)
	}

	if err := deps.Input.Click(button, clicks); err != nil {
		return toolError("%v", err)
	}
	x, y := deps.Input.AbsPosition()
	return mcp.NewToolResultText(fmt.Sprintf("Clicked %s %d time(s) at HID (%d, %d)", button, clicks, x, y)), nil
}

// A single isolated wheel report is dropped by macOS, so mouse_scroll sends a short
// gesture instead of one report: measured on a 1080p host, one report alone moved 0px
// while a run of them moved about 100px each. delta stays per report, which is the same
// meaning it has in mouse_scroll_burst. The gap matches defaultWheelIntervalMs: a
// shorter one lets the smooth-scroll animation of one report overlap the next, which
// makes the distance travelled vary by more than 50% between identical calls.
const (
	scrollGestureReports    = 3
	scrollGestureIntervalMs = 100
)

func handleMouseScroll(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	steps, wheelX, mouseMode, errResult := parseWheelArgs(req.GetArguments())
	if errResult != nil {
		return errResult, nil
	}

	sent := 0
	for i := 0; i < scrollGestureReports; i++ {
		if err := ctx.Err(); err != nil {
			return toolError("cancelled after %d of %d reports: %v", sent, scrollGestureReports, err)
		}
		if err := deps.Input.Scroll(int8(steps), int8(wheelX), mouseMode); err != nil {
			return toolError("report %d of %d failed after %d sent: %v", i+1, scrollGestureReports, sent, err)
		}
		sent++
		if i < scrollGestureReports-1 {
			time.Sleep(scrollGestureIntervalMs * time.Millisecond)
		}
	}

	if wheelX != 0 {
		return mcp.NewToolResultText(fmt.Sprintf("Scrolled %d×%d vertical, %d horizontal = %d steps (%s mode)",
			scrollGestureReports, steps, wheelX, steps*scrollGestureReports, mouseMode)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Scrolled %d×%d = %d steps (%s mode)",
		scrollGestureReports, steps, steps*scrollGestureReports, mouseMode)), nil
}

// handleMouseScrollBurst repeats the wheel report instead of sending one large
// value, because that is what a real wheel gesture looks like on the host: a
// measured gesture on macOS Chrome is 15-17 wheel events 17ms apart and every
// event moves the page about 70-76px. A single large report is one event, so it
// cannot travel the same distance as a gesture.
func handleMouseScrollBurst(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	steps, wheelX, mouseMode, errResult := parseWheelArgs(args)
	if errResult != nil {
		return errResult, nil
	}

	repeat := defaultWheelRepeat
	if v, ok := argFloat(args, "repeat"); ok {
		repeat = int(v)
	}

	// total is the distance to travel, repeat is the number of reports. Passing
	// total lets the caller ask for "two screens" without doing the division.
	if total, ok := argFloat(args, "total"); ok && total != 0 {
		if steps == 0 {
			return toolError("total needs a non-zero delta to derive the report count")
		}
		if (total > 0) != (steps > 0) {
			return toolError("total %g and delta %d must have the same sign", total, steps)
		}
		repeat = int(math.Round(math.Abs(total) / math.Abs(float64(steps))))
		if repeat < 1 {
			repeat = 1
		}
	}

	if repeat < 1 || repeat > maxWheelRepeat {
		return toolError("repeat %d is out of range, 1..%d", repeat, maxWheelRepeat)
	}

	interval := defaultWheelIntervalMs
	if v, ok := argFloat(args, "intervalMs"); ok {
		interval = int(v)
	}
	if interval < 0 || interval > maxWheelIntervalMs {
		return toolError("intervalMs %d is out of range, 0..%d", interval, maxWheelIntervalMs)
	}

	start := time.Now()
	sent := 0
	for i := 0; i < repeat; i++ {
		if err := ctx.Err(); err != nil {
			return toolError("cancelled after %d of %d reports: %v", sent, repeat, err)
		}
		if err := deps.Input.Scroll(int8(steps), int8(wheelX), mouseMode); err != nil {
			return toolError("report %d of %d failed after %d sent: %v", i+1, repeat, sent, err)
		}
		sent++
		if i < repeat-1 && interval > 0 {
			time.Sleep(time.Duration(interval) * time.Millisecond)
		}
	}

	return mcp.NewToolResultText(fmt.Sprintf("Scrolled %d×%d = %d steps in %dms (%dms apart, %s mode)",
		repeat, steps, steps*repeat, time.Since(start).Milliseconds(), interval, mouseMode)), nil
}

// parseWheelArgs validates the fields mouse_scroll and mouse_scroll_burst share.
func parseWheelArgs(args map[string]interface{}) (steps int, wheelX int, mouseMode string, errResult *mcp.CallToolResult) {
	delta, ok := argFloat(args, "delta")
	if !ok {
		return 0, 0, "", toolErrorResult("delta is required and must be a number")
	}
	steps = int(delta)
	if steps < -maxWheelSteps || steps > maxWheelSteps {
		return 0, 0, "", toolErrorResult("delta %d is out of range, at most %d wheel steps per report", steps, maxWheelSteps)
	}

	mouseMode = strings.ToLower(argString(args, "mouseMode"))
	if mouseMode == "" {
		mouseMode = "absolute"
	}
	if mouseMode != "absolute" && mouseMode != "relative" {
		return 0, 0, "", toolErrorResult("unknown mouseMode %q, use absolute or relative", mouseMode)
	}

	if v, ok := argFloat(args, "wheelX"); ok {
		wheelX = int(v)
	}
	if wheelX < -maxWheelSteps || wheelX > maxWheelSteps {
		return 0, 0, "", toolErrorResult("wheelX %d is out of range, at most %d wheel steps per report", wheelX, maxWheelSteps)
	}

	return steps, wheelX, mouseMode, nil
}

// withKeyCode adds a usage ID to the pressed set unless it is already there: a
// HID report must not carry the same key twice, so a repeated "down" has to be a
// no-op instead of a duplicate array entry.
func withKeyCode(keys []uint8, keyCode uint8) []uint8 {
	for _, held := range keys {
		if held == keyCode {
			return keys
		}
	}
	return append(keys, keyCode)
}

func handleKeyboardKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	keyName := argString(args, "key")

	// a modifier is not a key code: it lives in the report's modifier byte, which
	// is why keyboard_key used to reject ctrl and shift while the combo tool
	// accepted them
	modifier, isModifier := deps.Input.ModifierBit(keyName)
	keyCode, isKey := deps.Input.KeyCode(keyName)
	if !isModifier && !isKey {
		return toolError("unknown key %q, use a name like Enter, Escape, Tab, ArrowUp, F1, a, 1 "+
			"or a modifier (ctrl, shift, alt, win); use keyboard_combo for combinations "+
			"and for punctuation that needs Shift, such as + or ?", keyName)
	}

	action := strings.ToLower(argString(args, "action"))
	if action == "" {
		action = "press"
	}
	if action != "press" && action != "down" && action != "up" {
		return toolError("unknown action %q, use press, down or up", action)
	}

	return withInput("keyboard_key", defaultInputHold, func() (*mcp.CallToolResult, error) {
		var err error
		switch {
		case isModifier && action == "press":
			// The modifier byte carries every held modifier at once, so a tap sends the
			// merged set and then restores what was held before it: reporting the bit on
			// its own would release the other modifiers, and ReleaseKeys would drop the
			// held keys as well.
			held, keys := deps.Input.KeysDownState()
			if err = deps.Input.KeyboardReport(held|modifier, keys); err == nil {
				time.Sleep(30 * time.Millisecond)
				err = deps.Input.KeyboardReport(held, keys)
			}
		case isModifier && action == "down":
			// A report carries the whole modifier byte, so the new bit has to be
			// merged into what is already held. Sending it on its own silently
			// released a shift pressed on the other side.
			held, keys := deps.Input.KeysDownState()
			err = deps.Input.KeyboardReport(held|modifier, keys)
		case isModifier:
			held, keys := deps.Input.KeysDownState()
			err = deps.Input.KeyboardReport(held&^modifier, keys)
		case action == "press":
			// A plain key tap has to carry the held modifiers and the held keys: sending
			// it on its own released a shift that was down and wiped the pressed set,
			// which is the same lie the modifier paths above were fixed for.
			held, keys := deps.Input.KeysDownState()
			if err = deps.Input.KeyboardReport(held, withKeyCode(keys, keyCode)); err == nil {
				time.Sleep(30 * time.Millisecond)
				err = deps.Input.KeyboardReport(held, keys)
			}
		case action == "up":
			// A plain key release still sends an empty report: the pressed set is
			// shared by every key, so removing a single one cannot be expressed
			// without also releasing the others.
			err = deps.Input.ReleaseKeys()
		default:
			// down holds the key. Tapping here made keyboard_key lie: the caller
			// asked for a held key and keyboard_state read back empty.
			held, keys := deps.Input.KeysDownState()
			err = deps.Input.KeyboardReport(held, withKeyCode(keys, keyCode))
		}
		if err != nil {
			return toolError("%v", err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("%s key: %s", action, keyName)), nil
	})
}

func handleKeyboardCombo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	keysArg, _ := args["keys"].([]interface{})
	if len(keysArg) == 0 {
		return toolError("keys is required and must list at least one key")
	}
	keyNames := make([]string, 0, len(keysArg))
	for _, k := range keysArg {
		keyName, _ := k.(string)
		keyNames = append(keyNames, keyName)
	}

	modifier, keys, err := deps.Input.ParseCombo(keyNames)
	if err != nil {
		return toolError("%v", err)
	}
	if len(keys) == 0 && modifier == 0 {
		return toolError("keys contained no usable key name")
	}

	return withInput("keyboard_combo", defaultInputHold, func() (*mcp.CallToolResult, error) {
		if err := deps.Input.TapKeys(modifier, keys); err != nil {
			return toolError("%v", err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Pressed combo: %v", keyNames)), nil
	})
}

// modifierNames decodes a modifier byte into the names of its set bits, so a
// test can assert on "ShiftRight" instead of remembering that it is 0x20.
var modifierNames = []struct {
	bit  uint8
	name string
}{
	{0x01, "ControlLeft"},
	{0x02, "ShiftLeft"},
	{0x04, "AltLeft"},
	{0x08, "MetaLeft"},
	{0x10, "ControlRight"},
	{0x20, "ShiftRight"},
	{0x40, "AltRight"},
	{0x80, "MetaRight"},
}

func handleKeyboardState(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	modifier, keys := deps.Input.KeysDownState()

	names := make([]string, 0, len(modifierNames))
	for _, entry := range modifierNames {
		if modifier&entry.bit != 0 {
			names = append(names, entry.name)
		}
	}
	if len(names) == 0 {
		names = append(names, "none")
	}

	codes := make([]string, 0, len(keys))
	for _, code := range keys {
		if code == 0 {
			continue
		}
		codes = append(codes, fmt.Sprintf("0x%02X", code))
	}
	if len(codes) == 0 {
		codes = append(codes, "none")
	}

	leds := deps.Input.KeyboardLedState()
	ledNames := make([]string, 0, 6)
	for _, entry := range []struct {
		on   bool
		name string
	}{
		{leds.NumLock, "num"},
		{leds.CapsLock, "caps"},
		{leds.ScrollLock, "scroll"},
		{leds.Compose, "compose"},
		{leds.Kana, "kana"},
		{leds.Shift, "shift"},
	} {
		if entry.on {
			ledNames = append(ledNames, entry.name)
		}
	}
	if len(ledNames) == 0 {
		ledNames = append(ledNames, "none")
	}

	// The LED byte is host -> device only and the host sends it only on change,
	// so an unreported byte is the process's reset default, not the host's real
	// lights. Saying "none" there is a lie that costs an hour of debugging.
	ledText := fmt.Sprintf("0x%02X (%s)", leds.Raw, strings.Join(ledNames, "+"))
	if !leds.Reported {
		ledText = fmt.Sprintf("unknown (host has not sent an LED report since this process started; the reset default would read 0x%02X)", leds.Raw)
	}

	return mcp.NewToolResultText(fmt.Sprintf(
		"modifier=0x%02X (%s); keys=[%s]; leds=%s",
		modifier, strings.Join(names, "+"), strings.Join(codes, " "), ledText)), nil
}

func handleTypeText(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	text := argString(args, "text")
	if text == "" {
		return toolError("text is required")
	}

	delay := time.Duration(defaultTypeDelayMs) * time.Millisecond
	if value, ok := argFloat(args, "delayMs"); ok {
		delay = time.Duration(clampInt(int(value), 0, maxTypeDelayMs)) * time.Millisecond
	}

	// a typed string is the one call that keeps the keyboard for a long time,
	// so its lease is sized from the text and the delay
	hold := HoldForText(len([]rune(text)), delay)
	return withInput("type_text", hold, func() (*mcp.CallToolResult, error) {
		// TypeText validates the whole string before the first keystroke, so a
		// rejected character never leaves half of the text on the remote host.
		if err := deps.Input.TypeText(text, delay); err != nil {
			return toolError("%v", err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Typed %d characters with %dms between keystrokes",
			len([]rune(text)), delay.Milliseconds())), nil
	})
}

func handleCaptureScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	timeout := time.Duration(defaultShotTimeoutMs) * time.Millisecond
	if value, ok := argFloat(args, "timeoutMs"); ok {
		timeout = time.Duration(clampInt(int(value), minShotTimeoutMs, maxShotTimeoutMs)) * time.Millisecond
	}

	data, err := deps.Screen.Capture(timeout)
	if err != nil {
		return toolError("%v", err)
	}
	if len(data) == 0 {
		return toolError("screenshot returned no data")
	}

	// tell the model how big the picture is, so it knows whether a retry or a
	// crop makes sense
	state, stateErr := deps.Screen.State()
	text := fmt.Sprintf("JPEG screenshot: %dx%d, %d bytes", state.Width, state.Height, len(data))
	if stateErr != nil {
		text = fmt.Sprintf("JPEG screenshot: %d bytes", len(data))
	}

	// keep the capture on disk so it can be inspected later and report where it
	// landed; the native encoder only ever writes its own spool file
	if path, saveErr := deps.Screen.Save(data); saveErr == nil {
		text += fmt.Sprintf(", saved to %s", path)
	} else {
		log().Warn().Err(saveErr).Msg("failed to persist screenshot")
	}

	return mcp.NewToolResultImage(text, base64.StdEncoding.EncodeToString(data), "image/jpeg"), nil
}

func handleGetVideoState(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	state, err := deps.Screen.State()
	if err != nil {
		return toolError("failed to read video state: %v", err)
	}
	text := fmt.Sprintf("Video: %dx%d @ %.1f fps (Ready: %v)", state.Width, state.Height, state.FPS, state.Ready)
	if state.Error != "" {
		text += fmt.Sprintf(" [Error: %s]", state.Error)
	}
	return mcp.NewToolResultText(text), nil
}
