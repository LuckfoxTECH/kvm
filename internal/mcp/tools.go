package mcp

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Tool schemas. The descriptions are the only thing a model sees before it
// calls a tool, so every unit and every bound is spelled out here.

// hints describes how a tool behaves, which is what a client looks at before it
// decides whether to ask the user first. The MCP library fills every tool with
// destructive=true and readOnly=false, so a plain status query looked like it
// changed the host and a client could demand confirmation on every call.
type hints struct {
	readOnly    bool
	destructive bool
	idempotent  bool
}

func (h hints) options() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithReadOnlyHintAnnotation(h.readOnly),
		mcp.WithDestructiveHintAnnotation(h.destructive),
		mcp.WithIdempotentHintAnnotation(h.idempotent),
		// the tools only act on this device, they never reach an external entity
		mcp.WithOpenWorldHintAnnotation(false),
	}
}

// tool builds a tool with its behaviour hints attached.
func tool(name string, h hints, opts ...mcp.ToolOption) mcp.Tool {
	return mcp.NewTool(name, append(opts, h.options()...)...)
}

var (
	// stateQuery reads state and changes nothing, so a client may call it freely.
	stateQuery = hints{readOnly: true, idempotent: true}
	// moveInput repositions the pointer: not read-only, but repeating it with the
	// same arguments lands in the same place and it destroys nothing.
	moveInput = hints{idempotent: true}
	// inputInput injects an input event. It changes nothing on the device itself,
	// so it is not destructive, but a repeated call repeats the effect, so it is
	// neither read-only nor idempotent.
	inputInput = hints{}
)

func registerTools(s *server.MCPServer) {
	// the limit lives in the daemon, so the description has to ask for it instead
	// of duplicating the constant
	maxText := 1000
	if deps.Input != nil {
		maxText = deps.Input.MaxTextLength()
	}

	s.AddTool(tool("mouse_move", moveInput,
		mcp.WithDescription("Move the mouse. mode=absolute: x,y are pixels of the video input; mode=relative: dx,dy are pixel offsets of any size"),
		mcp.WithString("mode", mcp.Description("absolute (default) or relative"), mcp.Enum("absolute", "relative")),
		mcp.WithNumber("x", mcp.Description("Absolute mode: X pixel, 0..width-1, clamped")),
		mcp.WithNumber("y", mcp.Description("Absolute mode: Y pixel, 0..height-1, clamped")),
		mcp.WithNumber("dx", mcp.Description("Relative mode: X offset in pixels, -2047..2047")),
		mcp.WithNumber("dy", mcp.Description("Relative mode: Y offset in pixels, -2047..2047")),
	), auditTool("mouse_move", handleMouseMove))

	s.AddTool(tool("mouse_click", inputInput,
		mcp.WithDescription("Click a mouse button where the pointer currently is"),
		mcp.WithString("button", mcp.Required(), mcp.Enum("left", "right", "middle")),
		mcp.WithNumber("clicks", mcp.Description("Click count, 1..5, default 1")),
	), auditTool("mouse_click", handleMouseClick))

	s.AddTool(tool("mouse_scroll", inputInput,
		mcp.WithDescription("Scroll the mouse wheel. This is the tool to use for normal scrolling: it sends a short gesture of "+
			"3 reports 15ms apart, because a single isolated report is dropped by macOS and moved the page 0px. "+
			"delta is per report and travel scales linearly: measured on a 1080p host, delta=1 moves about 190px and "+
			"delta=3 about 590px. Use mouse_scroll_burst when you need an exact distance or a longer run"),
		mcp.WithNumber("delta", mcp.Required(), mcp.Description("Wheel steps per report, -100..100, negative scrolls down")),
		mcp.WithNumber("wheelX", mcp.Description("Horizontal wheel steps, -100..100; omit or 0 for vertical only")),
		mcp.WithString("mouseMode", mcp.Description("Advisory only: absolute (default) or relative. The device picks the report channel itself"), mcp.Enum("absolute", "relative")),
	), auditTool("mouse_scroll", handleMouseScroll))

	s.AddTool(tool("mouse_scroll_burst", inputInput,
		mcp.WithDescription("Scroll the wheel as a run of reports, the way a real wheel gesture arrives. Prefer mouse_scroll for "+
			"normal scrolling; use this one only when you need an exact report count, a custom spacing, or a longer run in a "+
			"single call. Measured on a 1080p macOS host: one report of delta=1 moves the page about 100px, so one screen "+
			"is about 10 reports and a real user gesture is 12-17 reports. Keep delta at 1 or 2 and use repeat "+
			"for distance: a large delta inside one report is capped by the host and barely moves the page"),
		mcp.WithNumber("delta", mcp.Required(), mcp.Description("Wheel steps per report, -100..100, negative scrolls down. Use 1 or 2; the host caps what one report can travel")),
		mcp.WithNumber("repeat", mcp.Description(fmt.Sprintf("Number of reports, 1..%d, default %d (about one screen at delta=1)", maxWheelRepeat, defaultWheelRepeat))),
		mcp.WithNumber("total", mcp.Description("Total wheel steps to travel; when given, repeat is derived as total/delta. One step is about 100px, so total=-10 is about one screen")),
		mcp.WithNumber("intervalMs", mcp.Description(fmt.Sprintf("Gap between two reports in ms, 0..%d, default %d (the gap a browser needs to scroll a full step; a shorter gap lets one report's smooth-scroll animation overlap the next and the distance becomes unpredictable)", maxWheelIntervalMs, defaultWheelIntervalMs))),
		mcp.WithNumber("wheelX", mcp.Description("Horizontal wheel steps, -100..100; omit or 0 for vertical only")),
		mcp.WithString("mouseMode", mcp.Description("Advisory only: absolute (default) or relative. The device picks the report channel itself"), mcp.Enum("absolute", "relative")),
	), auditTool("mouse_scroll_burst", handleMouseScrollBurst))

	s.AddTool(tool("keyboard_key", inputInput,
		mcp.WithDescription("Send a single key: press it, or hold it down / release it"),
		mcp.WithString("key", mcp.Required(), mcp.Description(
			"Key name, case-insensitive: Enter, Escape, Tab, Backspace, ArrowUp, F1, a, 1, or a modifier "+
				"(ctrl, shift, alt, win) to press or hold it alone. Use keyboard_combo for combinations.")),
		mcp.WithString("action", mcp.Description("press (default), down or up"), mcp.Enum("press", "down", "up")),
	), auditTool("keyboard_key", handleKeyboardKey))

	s.AddTool(tool("keyboard_combo", inputInput,
		mcp.WithDescription("Press a key combination, for example ctrl+alt+Delete"),
		mcp.WithArray("keys", mcp.Required(), mcp.Description("Key names, modifiers first. A single punctuation character works too, so ctrl and - is written [ctrl, -]; characters that need Shift on a US layout bring it with them, so ctrl and + is Ctrl+Shift+="), mcp.Items(map[string]any{"type": "string"})),
	), auditTool("keyboard_combo", handleKeyboardCombo))

	s.AddTool(tool("type_text", inputInput,
		mcp.WithDescription("Type an ASCII text string into the remote host"),
		mcp.WithString("text", mcp.Required(),
			mcp.Description(fmt.Sprintf("Text to type, ASCII only, at most %d characters", maxText))),
		mcp.WithNumber("delayMs", mcp.Description("Delay between two keystrokes in ms, 0..1000, default 30")),
	), auditTool("type_text", handleTypeText))

	s.AddTool(tool("keyboard_state", stateQuery,
		mcp.WithDescription("Read the keyboard state the device is currently reporting to the host: "+
			"modifier bitmask (0x02 left shift, 0x20 right shift, 0x40 right Alt/AltGr, ...), "+
			"the pressed HID key codes, and the LED byte the host last sent back "+
			"(lock lights of the remote keyboard)"),
	), auditTool("keyboard_state", handleKeyboardState))

	s.AddTool(tool("capture_screenshot", stateQuery,
		mcp.WithDescription("Capture a JPEG screenshot of the remote screen"),
		mcp.WithNumber("timeoutMs", mcp.Description("Capture timeout in ms, 1000..30000, default 5000")),
	), auditTool("capture_screenshot", handleCaptureScreenshot))

	s.AddTool(tool("get_video_state", stateQuery,
		mcp.WithDescription("Get screen resolution, frame rate and video status"),
	), auditTool("get_video_state", handleGetVideoState))
}
