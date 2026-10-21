package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
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
		// the HID relative field is signed 8-bit, clamp instead of wrapping around
		stepX := clampInt(int(dx), -127, 127)
		stepY := clampInt(int(dy), -127, 127)
		if err := deps.Input.MoveRelative(int8(stepX), int8(stepY)); err != nil {
			return toolError("%v", err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Mouse moved by (%d, %d)", stepX, stepY)), nil
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

func handleMouseScroll(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	delta, ok := argFloat(args, "delta")
	if !ok {
		return toolError("delta is required and must be a number")
	}
	steps := int(delta)
	if steps < -maxWheelSteps || steps > maxWheelSteps {
		return toolError("delta %d is out of range, at most %d wheel steps per call", steps, maxWheelSteps)
	}

	mouseMode := strings.ToLower(argString(args, "mouseMode"))
	if mouseMode == "" {
		mouseMode = "absolute"
	}
	if mouseMode != "absolute" && mouseMode != "relative" {
		return toolError("unknown mouseMode %q, use absolute or relative", mouseMode)
	}

	if err := deps.Input.Scroll(int8(steps), mouseMode); err != nil {
		return toolError("%v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("Scrolled by %d (%s mode)", steps, mouseMode)), nil
}

func handleKeyboardKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	keyName := argString(args, "key")

	keyCode, ok := deps.Input.KeyCode(keyName)
	if !ok {
		return toolError("unknown key %q, use names like Enter, Escape, Tab, Backspace, ctrl, a, 1, F1", keyName)
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
		switch action {
		case "press":
			err = deps.Input.TapKeys(0, []uint8{keyCode})
		case "down":
			err = deps.Input.KeyboardReport(0, []uint8{keyCode})
		case "up":
			err = deps.Input.ReleaseKeys()
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
