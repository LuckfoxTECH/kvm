package mcp

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Tool schemas. The descriptions are the only thing a model sees before it
// calls a tool, so every unit and every bound is spelled out here.

func registerTools(s *server.MCPServer) {
	// the limit lives in the daemon, so the description has to ask for it instead
	// of duplicating the constant
	maxText := 1000
	if deps.Input != nil {
		maxText = deps.Input.MaxTextLength()
	}

	s.AddTool(mcp.NewTool("mouse_move",
		mcp.WithDescription("Move the mouse. mode=absolute: x,y are pixels of the video input; mode=relative: dx,dy are HID steps"),
		mcp.WithString("mode", mcp.Description("absolute (default) or relative"), mcp.Enum("absolute", "relative")),
		mcp.WithNumber("x", mcp.Description("Absolute mode: X pixel, 0..width-1, clamped")),
		mcp.WithNumber("y", mcp.Description("Absolute mode: Y pixel, 0..height-1, clamped")),
		mcp.WithNumber("dx", mcp.Description("Relative mode: X offset in HID steps, clamped to -127..127")),
		mcp.WithNumber("dy", mcp.Description("Relative mode: Y offset in HID steps, clamped to -127..127")),
	), auditTool("mouse_move", handleMouseMove))

	s.AddTool(mcp.NewTool("mouse_click",
		mcp.WithDescription("Click a mouse button where the pointer currently is"),
		mcp.WithString("button", mcp.Required(), mcp.Enum("left", "right", "middle")),
		mcp.WithNumber("clicks", mcp.Description("Click count, 1..5, default 1")),
	), auditTool("mouse_click", handleMouseClick))

	s.AddTool(mcp.NewTool("mouse_scroll",
		mcp.WithDescription("Scroll the mouse wheel"),
		mcp.WithNumber("delta", mcp.Required(), mcp.Description("Wheel steps, -100..100, negative scrolls down")),
		mcp.WithString("mouseMode", mcp.Description("Mouse mode of the target machine: absolute (default) or relative"), mcp.Enum("absolute", "relative")),
	), auditTool("mouse_scroll", handleMouseScroll))

	s.AddTool(mcp.NewTool("keyboard_key",
		mcp.WithDescription("Send a single key: press it, or hold it down / release it"),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key name: Enter, Escape, Tab, Backspace, F1..F12, a-z, 0-9, etc.")),
		mcp.WithString("action", mcp.Description("press (default), down or up"), mcp.Enum("press", "down", "up")),
	), auditTool("keyboard_key", handleKeyboardKey))

	s.AddTool(mcp.NewTool("keyboard_combo",
		mcp.WithDescription("Press a key combination, for example ctrl+alt+Delete"),
		mcp.WithArray("keys", mcp.Required(), mcp.Description("Key names, modifiers first"), mcp.Items(map[string]any{"type": "string"})),
	), auditTool("keyboard_combo", handleKeyboardCombo))

	s.AddTool(mcp.NewTool("type_text",
		mcp.WithDescription("Type an ASCII text string into the remote host"),
		mcp.WithString("text", mcp.Required(),
			mcp.Description(fmt.Sprintf("Text to type, ASCII only, at most %d characters", maxText))),
		mcp.WithNumber("delayMs", mcp.Description("Delay between two keystrokes in ms, 0..1000, default 30")),
	), auditTool("type_text", handleTypeText))

	s.AddTool(mcp.NewTool("capture_screenshot",
		mcp.WithDescription("Capture a JPEG screenshot of the remote screen"),
		mcp.WithNumber("timeoutMs", mcp.Description("Capture timeout in ms, 1000..30000, default 5000")),
	), auditTool("capture_screenshot", handleCaptureScreenshot))

	s.AddTool(mcp.NewTool("get_video_state",
		mcp.WithDescription("Get screen resolution, frame rate and video status"),
	), auditTool("get_video_state", handleGetVideoState))
}
