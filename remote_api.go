package kvm

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// This file holds both HTTP surfaces that control the device over the network
// stack, side by side, on top of the primitives in input_actions.go:
//
//   - the local CLI API, reachable only through a unix socket by processes on
//     this device (see cli.go)
//   - the MCP server, reachable over TCP and guarded by the MCP API key
//
// Keeping them in one file makes a fix on one surface impossible to miss on the
// other.

// cliAPISocketPath is where the local CLI API listens. A unix socket keeps the
// HID control surface off the network entirely: no port to scan, no CORS, and
// the filesystem permission is the access control.
const cliAPISocketPath = "/var/run/kvm_cli.sock"

// === Local CLI API ===

// StartAPIServer runs the local CLI API. It is intentionally unauthenticated:
// every caller is a process on this device that can already open the socket.
// Remote callers must use the MCP server instead.
func StartAPIServer(socketPath string) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"device":  GetDeviceID(),
			"version": builtAppVersion,
		})
	})

	api := r.Group("/api/lan")
	{
		// Mouse
		api.POST("/mouse/absolute", handleAPIMouseAbsolute)
		api.POST("/mouse/relative", handleAPIMouseRelative)
		api.POST("/mouse/click", handleAPIMouseClick)
		api.POST("/mouse/scroll", handleAPIMouseScroll)

		// Keyboard
		api.POST("/keyboard/key", handleAPIKeyboardKey)
		api.POST("/keyboard/combo", handleAPIKeyboardCombo)
		api.POST("/keyboard/type", handleAPIKeyboardType)

		// Capture
		api.GET("/capture", handleAPICapture)

		// Status
		api.GET("/status", handleAPIStatus)
		api.GET("/video/state", handleAPIVideoState)
	}

	// a socket file left behind by a previous run would make Listen fail
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		logger.Error().Err(err).Str("socket", socketPath).Msg("local CLI API listener failed")
		return
	}
	// only processes running as the daemon's user may drive the HID gadget
	if err := os.Chmod(socketPath, 0o600); err != nil {
		logger.Warn().Err(err).Str("socket", socketPath).Msg("failed to restrict CLI API socket permissions")
	}

	logger.Info().Str("socket", socketPath).Msg("Starting local CLI API server")
	if err := http.Serve(listener, r); err != nil {
		logger.Error().Err(err).Msg("local CLI API server failed")
	}
}

// === MCP server ===

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
	// always mount auth: a missing key must deny access, never expose the server
	handler = withAPIKeyAuth(handler, config.APIKey)
	handler = withCORS(handler)

	logger.Info().Str("addr", addr).Msg("Starting MCP SSE server")
	if err := http.ListenAndServe(addr, handler); err != nil {
		logger.Error().Err(err).Msg("MCP SSE server failed")
	}
}

// === HTTP helpers ===

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

// withAPIKeyAuth guards the MCP endpoints with the MCP API key. No caller is
// exempt, including loopback ones, because the MCP port is reachable from the
// network and any local process could otherwise take control of the device.
// An empty configured key means "not provisioned" and denies every request.
func withAPIKeyAuth(next http.Handler, expectedKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expectedKey == "" {
			writeJSONError(w, http.StatusUnauthorized, "MCP API key not configured")
			return
		}

		key, ok := extractBearer(r.Header.Get("Authorization"))
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
			return
		}
		// constant-time comparison, the key is a secret
		if subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "invalid api key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractBearer(header string) (string, bool) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":%q}`, message)
}

// === Local CLI API handlers ===

// handleAPIMouseAbsolute takes raw HID logical units (0-32767), not pixels.
func handleAPIMouseAbsolute(c *gin.Context) {
	var req struct {
		X       int   `json:"x" binding:"required"`
		Y       int   `json:"y" binding:"required"`
		Buttons uint8 `json:"buttons"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := absMouse(req.X, req.Y, req.Buttons); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "x": req.X, "y": req.Y})
}

func handleAPIMouseRelative(c *gin.Context) {
	var req struct {
		Dx      int8  `json:"dx" binding:"required"`
		Dy      int8  `json:"dy" binding:"required"`
		Buttons uint8 `json:"buttons"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := relMouse(req.Dx, req.Dy, req.Buttons); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "dx": req.Dx, "dy": req.Dy})
}

func handleAPIMouseClick(c *gin.Context) {
	var req struct {
		Button string `json:"button" binding:"required"`
		Double bool   `json:"double"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	clicks := 1
	if req.Double {
		clicks = 2
	}
	if err := clickMouse(req.Button, clicks); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "button": req.Button, "clicks": clicks})
}

func handleAPIMouseScroll(c *gin.Context) {
	var req struct {
		Delta int8 `json:"delta" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := wheelScroll(req.Delta, mouseModeAbsolute); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "delta": req.Delta})
}

func handleAPIKeyboardKey(c *gin.Context) {
	var req struct {
		Key    string `json:"key" binding:"required"`
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	keyCode, ok := keyNameToCode[req.Key]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown key: %s", req.Key)})
		return
	}

	action := strings.ToLower(req.Action)
	if action == "" {
		action = "press"
	}

	var err error
	switch action {
	case "press":
		err = tapKeys(0, []uint8{keyCode})
	case "down":
		err = keyboardReport(0, []uint8{keyCode})
	case "up":
		err = releaseKeys()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown action: %s", req.Action)})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "key": req.Key, "action": action})
}

func handleAPIKeyboardCombo(c *gin.Context) {
	var req struct {
		Keys []string `json:"keys" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	modifier, keys, err := parseCombo(req.Keys)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := tapKeys(modifier, keys); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "keys": req.Keys})
}

func handleAPIKeyboardType(c *gin.Context) {
	var req struct {
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := typeASCIIText(req.Text); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "text": req.Text})
}

// handleAPICapture returns raw JPEG bytes, the CLI writes them to a file.
func handleAPICapture(c *gin.Context) {
	data, err := captureScreenshot("jpeg")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "image/jpeg", data)
}

func handleAPIStatus(c *gin.Context) {
	state, err := rpcGetVideoState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"video": gin.H{
			"ready":  state.Ready,
			"width":  state.Width,
			"height": state.Height,
			"fps":    state.FramePerSecond,
			"error":  state.Error,
		},
		"device": gin.H{
			"name":    "PicoKVM",
			"version": builtAppVersion,
		},
	})
}

func handleAPIVideoState(c *gin.Context) {
	state, err := rpcGetVideoState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ready":  state.Ready,
		"width":  state.Width,
		"height": state.Height,
		"fps":    state.FramePerSecond,
		"error":  state.Error,
	})
}

// === MCP tool registration ===

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

// === MCP handlers ===

func handleMouseMoveAbsolute(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	x, _ := args["x"].(float64)
	y, _ := args["y"].(float64)

	px, py, err := moveMouseToPixel(int(x), int(y))
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Mouse moved to pixel (%d, %d)", px, py)), nil
}

func handleMouseMoveRelative(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	dx, _ := args["dx"].(float64)
	dy, _ := args["dy"].(float64)

	// the HID relative field is signed 8-bit, clamp instead of wrapping around
	stepX := clampInt(int(dx), -127, 127)
	stepY := clampInt(int(dy), -127, 127)
	if err := relMouse(int8(stepX), int8(stepY), 0); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Mouse moved by (%d, %d)", stepX, stepY)), nil
}

func handleMouseClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	button, _ := args["button"].(string)

	if err := clickMouse(button, 1); err != nil {
		return nil, err
	}
	x, y := gadget.GetAbsMousePosition()
	return mcp.NewToolResultText(fmt.Sprintf("Clicked %s button at HID (%d, %d)", button, x, y)), nil
}

func handleMouseScroll(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	delta, _ := args["delta"].(float64)
	mouseMode, _ := args["mouseMode"].(string)

	// the HID wheel field is signed 8-bit, clamp instead of wrapping around
	steps := clampInt(int(delta), -127, 127)
	if err := wheelScroll(int8(steps), mouseMode); err != nil {
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
	if err := tapKeys(0, []uint8{keyCode}); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Pressed key: %s", keyName)), nil
}

func handleKeyboardCombo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	keysArg, _ := args["keys"].([]interface{})

	keyNames := make([]string, 0, len(keysArg))
	for _, k := range keysArg {
		keyName, _ := k.(string)
		keyNames = append(keyNames, keyName)
	}

	modifier, keys, err := parseCombo(keyNames)
	if err != nil {
		return nil, err
	}
	if err := tapKeys(modifier, keys); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(fmt.Sprintf("Pressed combo: %v", keyNames)), nil
}

func handleTypeText(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	text, _ := args["text"].(string)

	if err := typeASCIIText(text); err != nil {
		return nil, err
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
	state, err := rpcGetVideoState()
	if err != nil {
		return nil, err
	}
	text := fmt.Sprintf("Video: %dx%d @ %.1f fps (Ready: %v)", state.Width, state.Height, state.FramePerSecond, state.Ready)
	if state.Error != "" {
		text += fmt.Sprintf(" [Error: %s]", state.Error)
	}
	return mcp.NewToolResultText(text), nil
}
