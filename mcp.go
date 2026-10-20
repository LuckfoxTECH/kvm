package kvm

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func StartMCP(port int, stdio bool) {
	s := server.NewMCPServer("picokvm-mcp", "1.0.0")
	registerMCPTools(s)

	if stdio {
		logger.Info().Msg("Starting MCP stdio server")
		if err := server.ServeStdio(s); err != nil {
			logger.Error().Err(err).Msg("MCP stdio server failed")
		}
		return
	}

	// SSE mode
	addr := fmt.Sprintf(":%d", port)
	sseServer := server.NewSSEServer(s)

	mux := http.NewServeMux()
	mux.Handle("/sse", sseServer.SSEHandler())
	mux.Handle("/message", sseServer.MessageHandler())

	var handler http.Handler = mux
	if config.APIKey != "" {
		handler = withAPIKeyAuth(handler, config.APIKey)
	}
	handler = withCORS(handler)

	logger.Info().Str("addr", addr).Msg("Starting MCP SSE server")
	if err := http.ListenAndServe(addr, handler); err != nil {
		logger.Error().Err(err).Msg("MCP SSE server failed")
	}
}

// === Shared middleware helpers ===

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withAPIKeyAuth(next http.Handler, expectedKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for localhost
		if strings.HasPrefix(r.RemoteAddr, "127.0.0.1:") || 
		   strings.HasPrefix(r.RemoteAddr, "[::1]:") {
			next.ServeHTTP(w, r)
			return
		}
		
		auth := r.Header.Get("Authorization")
		var key string
		if _, err := fmt.Sscanf(auth, "Bearer %s", &key); err != nil {
			http.Error(w, `{"error":"missing or invalid authorization"}`, http.StatusUnauthorized)
			return
		}
		if !strings.EqualFold(key, expectedKey) {
			http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// === MCP Tool Registration ===

func registerMCPTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("mouse_move_absolute",
		mcp.WithDescription("Move mouse to absolute pixel coordinates of the video input (0..width-1, 0..height-1)"),
		mcp.WithNumber("x", mcp.Required(), mcp.Description("X pixel coordinate")),
		mcp.WithNumber("y", mcp.Required(), mcp.Description("Y pixel coordinate")),
	), handleMouseMoveAbsolute)

	s.AddTool(mcp.NewTool("mouse_move_relative",
		mcp.WithDescription("Move mouse by relative offset"),
		mcp.WithNumber("dx", mcp.Required()),
		mcp.WithNumber("dy", mcp.Required()),
	), handleMouseMoveRelative)

	s.AddTool(mcp.NewTool("mouse_click",
		mcp.WithDescription("Click mouse button"),
		mcp.WithString("button", mcp.Required(), mcp.Enum("left", "right", "middle")),
	), handleMouseClick)

	s.AddTool(mcp.NewTool("mouse_scroll",
		mcp.WithDescription("Scroll mouse wheel"),
		mcp.WithNumber("delta", mcp.Required(), mcp.Description("Wheel steps, clamped to -127..127")),
		mcp.WithString("mouseMode", mcp.Description("Mouse mode of the target machine: absolute (default) or relative"), mcp.Enum("absolute", "relative")),
	), handleMouseScroll)

	s.AddTool(mcp.NewTool("keyboard_key",
		mcp.WithDescription("Press a key"),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key name: Enter, Escape, Tab, etc.")),
	), handleKeyboardKey)

	s.AddTool(mcp.NewTool("keyboard_combo",
		mcp.WithDescription("Press key combination"),
		mcp.WithArray("keys", mcp.Required(), mcp.Items(map[string]any{"type": "string"})),
	), handleKeyboardCombo)

	s.AddTool(mcp.NewTool("type_text",
		mcp.WithDescription("Type an ASCII text string"),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to type, ASCII only")),
	), handleTypeText)

	s.AddTool(mcp.NewTool("capture_screenshot",
		mcp.WithDescription("Capture JPEG screenshot using hardware encoder"),
	), handleCaptureScreenshot)

	s.AddTool(mcp.NewTool("get_video_state",
		mcp.WithDescription("Get screen resolution and video status"),
	), handleGetVideoState)
}

// === MCP Handlers ===

func handleMouseMoveAbsolute(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	x, _ := args["x"].(float64)
	y, _ := args["y"].(float64)

	state, err := rpcGetVideoState()
	if err != nil {
		return nil, err
	}
	if state.Width < 2 || state.Height < 2 {
		return nil, fmt.Errorf("video resolution unknown (%dx%d), cannot map pixel coordinates", state.Width, state.Height)
	}

	// clamp into the visible area, then scale pixels to HID logical units (0-32767)
	maxX := state.Width - 1
	maxY := state.Height - 1
	px := int(x)
	py := int(y)
	if px < 0 {
		px = 0
	} else if px > maxX {
		px = maxX
	}
	if py < 0 {
		py = 0
	} else if py > maxY {
		py = maxY
	}
	absX := (px*32767 + maxX/2) / maxX
	absY := (py*32767 + maxY/2) / maxY

	_, err = callRPCHandler(rpcHandlers["absMouseReport"], map[string]interface{}{
		"x": absX, "y": absY, "buttons": 0,
	})
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Mouse moved to pixel (%d, %d)", px, py)), nil
}

func handleMouseMoveRelative(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	dx, _ := args["dx"].(float64)
	dy, _ := args["dy"].(float64)
	_, err := callRPCHandler(rpcHandlers["relMouseReport"], map[string]interface{}{
		"dx": dx, "dy": dy, "buttons": 0,
	})
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Mouse moved by (%d, %d)", int(dx), int(dy))), nil
}

func handleMouseClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	button, _ := args["button"].(string)
	var buttons uint8
	switch button {
	case "left":
		buttons = 1
	case "right":
		buttons = 2
	case "middle":
		buttons = 4
	default:
		return nil, fmt.Errorf("unknown button: %s", button)
	}

	// absolute reports always carry a position, so click where the pointer already is
	// instead of teleporting it to (0, 0)
	x, y := gadget.GetAbsMousePosition()

	_, err := callRPCHandler(rpcHandlers["absMouseReport"], map[string]interface{}{
		"x": x, "y": y, "buttons": buttons,
	})
	if err != nil {
		return nil, err
	}
	_, err = callRPCHandler(rpcHandlers["absMouseReport"], map[string]interface{}{
		"x": x, "y": y, "buttons": 0,
	})
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Clicked %s button at HID (%d, %d)", button, x, y)), nil
}

func handleMouseScroll(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	delta, _ := args["delta"].(float64)
	mouseMode, _ := args["mouseMode"].(string)
	if mouseMode == "" {
		mouseMode = "absolute"
	}

	// the HID wheel field is signed 8-bit, clamp instead of wrapping around
	steps := int(delta)
	if steps > 127 {
		steps = 127
	} else if steps < -127 {
		steps = -127
	}

	_, err := callRPCHandler(rpcHandlers["wheelReport"], map[string]interface{}{
		"wheelY": steps, "mouseMode": mouseMode,
	})
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Scrolled by %d (%s mode)", steps, mouseMode)), nil
}

func handleKeyboardKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	keyName, _ := args["key"].(string)
	keyCode, ok := keyNameToCode[keyName]
	if !ok {
		return nil, fmt.Errorf("unknown key: %s", keyName)
	}
	_, err := callRPCHandler(rpcHandlers["keyboardReport"], map[string]interface{}{
		"modifier": 0, "keys": []uint8{keyCode},
	})
	if err != nil {
		return nil, err
	}
	_, err = callRPCHandler(rpcHandlers["keyboardReport"], map[string]interface{}{
		"modifier": 0, "keys": []uint8{},
	})
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Pressed key: %s", keyName)), nil
}

func handleKeyboardCombo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	keysArg, _ := args["keys"].([]interface{})
	var keys []uint8
	var modifier uint8

	for _, k := range keysArg {
		keyName, _ := k.(string)
		switch strings.ToLower(keyName) {
		case "ctrl", "control":
			modifier |= 0x01
			continue
		case "shift":
			modifier |= 0x02
			continue
		case "alt":
			modifier |= 0x04
			continue
		case "meta", "win", "cmd":
			modifier |= 0x08
			continue
		}
		keyCode, ok := keyNameToCode[keyName]
		if !ok {
			return nil, fmt.Errorf("unknown key: %s", keyName)
		}
		keys = append(keys, keyCode)
	}

	_, err := callRPCHandler(rpcHandlers["keyboardReport"], map[string]interface{}{
		"modifier": modifier, "keys": keys,
	})
	if err != nil {
		return nil, err
	}
	_, err = callRPCHandler(rpcHandlers["keyboardReport"], map[string]interface{}{
		"modifier": 0, "keys": []uint8{},
	})
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Pressed combo: %v", keysArg)), nil
}

func handleTypeText(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	text, _ := args["text"].(string)
	for i, char := range text {
		if char > 0x7F {
			return nil, fmt.Errorf("unsupported non-ASCII character %q at byte index %d, only ASCII text can be typed", char, i)
		}
		keyCode, modifier, ok := charToKeyCode(uint8(char))
		if !ok {
			continue
		}
		_, err := callRPCHandler(rpcHandlers["keyboardReport"], map[string]interface{}{
			"modifier": modifier, "keys": []uint8{keyCode},
		})
		if err != nil {
			return nil, err
		}
		_, err = callRPCHandler(rpcHandlers["keyboardReport"], map[string]interface{}{
			"modifier": 0, "keys": []uint8{},
		})
		if err != nil {
			return nil, err
		}
	}
	return mcp.NewToolResultText(fmt.Sprintf("Typed: %s", text)), nil
}

func handleCaptureScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	data, err := captureScreenshot("jpeg")
	if err != nil {
		return nil, err
	}
	base64Data := base64.StdEncoding.EncodeToString(data)
	return mcp.NewToolResultImage("JPEG screenshot captured", base64Data, "image/jpeg"), nil
}

func handleGetVideoState(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, err := callRPCHandler(rpcHandlers["getVideoState"], nil)
	if err != nil {
		return nil, err
	}
	state, ok := result.(VideoInputState)
	if !ok {
		return nil, fmt.Errorf("unexpected video state type")
	}
	text := fmt.Sprintf("Video: %dx%d @ %.1f fps (Ready: %v)", state.Width, state.Height, state.FramePerSecond, state.Ready)
	if state.Error != "" {
		text += fmt.Sprintf(" [Error: %s]", state.Error)
	}
	return mcp.NewToolResultText(text), nil
}
