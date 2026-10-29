package kvm

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"kvm/internal/mcp"
)

// Local CLI API.
//
// The CLI (cli.go) runs on this device and drives the very same primitives in
// input_actions.go that the MCP tools use. It listens on a unix socket instead
// of a TCP port: nothing to scan from the network, no CORS, and the filesystem
// permission is the access control.

// cliAPISocketPath is where the local CLI API listens.
const cliAPISocketPath = "/var/run/kvm_cli.sock"

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

		// MCP management. The web server exposes the same three endpoints behind
		// the login session; this copy lets a local operator inspect the state,
		// disable the server or rotate the key when the Web UI is unreachable.
		// The 0600 socket is the access control here.
		api.GET("/mcp/config", handleMCPGetConfig)
		api.POST("/mcp/config", handleMCPSetConfig)
		api.POST("/mcp/key/regenerate", handleMCPRegenerateKey)
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

// === Local CLI API handlers ===

// handleAPIMouseAbsolute moves the pointer to a pixel position of the video
// input. Coordinates are pixels in the current video resolution, the same unit
// the MCP mouse_move tool takes; they are clamped to the screen and scaled to HID
// logical units internally, so a caller never has to know the 0..32767 protocol.
func handleAPIMouseAbsolute(c *gin.Context) {
	var req struct {
		X int `json:"x" binding:"required"`
		Y int `json:"y" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	px, py, err := moveMouseToPixel(req.X, req.Y)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "x": px, "y": py})
}

// handleAPIMouseRelative moves the pointer by a signed step. The unit is the HID
// relative field itself (-127..127 per report), which is what the MCP mouse_move
// mode=relative tool takes as well: a pixel offset has no meaning here without
// knowing the target's pointer acceleration. Out-of-range values are clamped
// instead of wrapping.
func handleAPIMouseRelative(c *gin.Context) {
	var req struct {
		Dx      int   `json:"dx" binding:"required"`
		Dy      int   `json:"dy" binding:"required"`
		Buttons uint8 `json:"buttons"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// clamp instead of letting the cast wrap around, the HID field is signed 8-bit
	dx := int8(clampInt(req.Dx, -127, 127))
	dy := int8(clampInt(req.Dy, -127, 127))
	if err := relMouse(dx, dy, req.Buttons); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "dx": dx, "dy": dy})
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
		Delta  int8 `json:"delta" binding:"required"`
		WheelX int8 `json:"wheelX"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := wheelScroll(req.Delta, req.WheelX, mouseModeAbsolute); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "delta": req.Delta, "wheelX": req.WheelX})
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
	keyCode, ok := lookupKeyCode(req.Key)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown key: %s", req.Key)})
		return
	}

	action := strings.ToLower(req.Action)
	if action == "" {
		action = "press"
	}

	// the input lease is shared with the MCP tools and the Web UI, so a local
	// operator cannot interleave keystrokes with a remote typed string
	release, err := mcp.AcquireInput(mcp.OwnerCLI, mcp.DefaultInputHold)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	defer release()

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

	release, err := mcp.AcquireInput(mcp.OwnerCLI, mcp.DefaultInputHold)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	defer release()

	if err := tapKeys(modifier, keys); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "keys": req.Keys})
}

func handleAPIKeyboardType(c *gin.Context) {
	var req struct {
		Text string `json:"text" binding:"required"`
		// nil means "use the default rhythm", an explicit 0 types as fast as
		// the HID stack accepts
		DelayMs *int `json:"delayMs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	delayMs := mcp.DefaultTypeDelayMs
	if req.DelayMs != nil {
		delayMs = clampInt(*req.DelayMs, 0, mcp.MaxTypeDelayMs)
	}
	delay := time.Duration(delayMs) * time.Millisecond
	// a long string keeps the keyboard for as long as it takes to type it
	release, err := mcp.AcquireInput(mcp.OwnerCLI, mcp.HoldForText(len([]rune(req.Text)), delay))
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	defer release()

	if err := typeASCIIText(req.Text, delay); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"text":    req.Text,
		"message": fmt.Sprintf("Typed %d characters with %dms between keystrokes",
			len([]rune(req.Text)), delayMs),
	})
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

// === MCP management (local socket copies) ===

func handleMCPGetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, mcp.ConfigView())
}

func handleMCPSetConfig(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "enabled (bool) is required"})
		return
	}
	if err := mcp.SetEnabled(*req.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	logger.Info().Bool("enabled", *req.Enabled).Msg("MCP server toggled via local CLI API")
	c.JSON(http.StatusOK, mcp.ConfigView())
}

func handleMCPRegenerateKey(c *gin.Context) {
	key, err := mcp.RegenerateKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	logger.Info().Msg("MCP API key regenerated via local CLI API")
	c.JSON(http.StatusOK, gin.H{"apiKey": key})
}
