// Package mcp implements the Model Context Protocol endpoint that lets an AI
// client drive the device.
//
// The package owns everything reachable from the network: the tool schemas, the
// API key middleware, the CORS and body-size guards, the input lease and the
// tool call audit log. It reaches the device only through the small interfaces
// below, so it never imports the daemon package and the daemon stays free to
// decide how a HID report is actually produced.
package mcp

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

// Limits shared by the tool schemas, the handlers and the HTTP guards.
const (
	// DefaultPort is the TCP port the SSE transport listens on.
	DefaultPort = 8081

	// MaxRequestBytes caps one MCP HTTP request body so a malformed or hostile
	// client cannot make the daemon allocate unbounded memory.
	MaxRequestBytes = 1 << 20 // 1 MiB

	// DefaultInputHold is how long a single key event keeps the input lease.
	DefaultInputHold = 10 * time.Second

	// DefaultTypeDelayMs is the default delay between two keystrokes.
	DefaultTypeDelayMs = defaultTypeDelayMs

	// MaxTypeDelayMs is the largest accepted delay between two keystrokes.
	MaxTypeDelayMs = maxTypeDelayMs

	// Typing rhythm: the remote USB HID stack drops keys when reports are sent
	// back to back, so type_text waits between two keystrokes.
	defaultTypeDelayMs = 30
	maxTypeDelayMs     = 1000

	maxClickCount = 5

	// maxWheelSteps bounds a single wheel report. The HID field is signed 8-bit
	// and an out-of-range value only makes the remote host scroll
	// unpredictably, so it is rejected instead of clamped silently.
	maxWheelSteps = 100

	defaultShotTimeoutMs = 5000
	minShotTimeoutMs     = 1000
	maxShotTimeoutMs     = 30000

	// The input lease is taken around every keyboard-class write. A single key
	// event needs a few milliseconds, a typed string needs as long as the text
	// takes; neither may hold the device forever.
	defaultInputHold = 10 * time.Second
	maxInputHold     = 60 * time.Second
)

// VideoState is the part of the video input state the tools report.
type VideoState struct {
	Width  int
	Height int
	FPS    float64
	Ready  bool
	Error  string
}

// Input is the HID surface of the device. Every method maps to one primitive in
// the daemon's input_actions.go, which is also what the local CLI API calls, so
// both surfaces keep identical semantics.
type Input interface {
	// MoveToPixel moves the absolute pointer to a pixel of the video input and
	// returns the coordinate after clamping.
	MoveToPixel(x, y int) (px, py int, err error)
	// MoveRelative moves the pointer by one signed 8-bit HID step.
	MoveRelative(dx, dy int8) error
	// Click clicks the given button where the pointer already is.
	Click(button string, clicks int) error
	// Scroll sends wheel steps for the given mouse mode.
	Scroll(steps int8, mouseMode string) error
	// KeyCode resolves a key name to its HID code.
	KeyCode(name string) (uint8, bool)
	// KeyboardReport sends a report without releasing it, so a key can be held.
	KeyboardReport(modifier uint8, keys []uint8) error
	// TapKeys presses the keys and releases everything again.
	TapKeys(modifier uint8, keys []uint8) error
	// ReleaseKeys releases every pressed key.
	ReleaseKeys() error
	// ParseCombo splits key names into modifier bits and key codes.
	ParseCombo(names []string) (modifier uint8, keys []uint8, err error)
	// TypeText types ASCII text with the given delay between keystrokes.
	TypeText(text string, delay time.Duration) error
	// AbsPosition returns the pointer position in HID logical units.
	AbsPosition() (x, y int)
	// MaxTextLength is the longest string one TypeText call accepts.
	MaxTextLength() int
}

// Screen is the capture surface of the device.
type Screen interface {
	// Capture returns one JPEG frame, giving up after the timeout.
	Capture(timeout time.Duration) ([]byte, error)
	// Save stores a capture on disk and returns its path.
	Save(data []byte) (string, error)
	// State returns the current video input state.
	State() (VideoState, error)
}

// KeyStore persists the MCP settings. The live copies stay in this package so
// the middleware never races with the config struct.
type KeyStore interface {
	// APIKey returns the key stored in the saved config.
	APIKey() string
	// Enabled reports whether the server is enabled in the saved config.
	Enabled() bool
	// SetEnabled saves the new switch value.
	SetEnabled(enabled bool) error
	// RegenerateKey creates and saves a new API key, returning the plaintext.
	RegenerateKey() (string, error)
}

// Deps is what the package needs from the daemon.
type Deps struct {
	Input  Input
	Screen Screen
	Key    KeyStore
	Log    *zerolog.Logger

	// AuditPath is the append-only file that receives one JSON line per tool
	// call. The daemon log is a live view for a developer session, this file is
	// the durable record, so the trail survives a log level change or a reboot.
	// An empty path keeps the audit in the log only.
	AuditPath string
}

var deps Deps

// Configure installs the device side. It must be called once before Start.
func Configure(d Deps) {
	deps = d
}

func log() *zerolog.Logger {
	if deps.Log == nil {
		nop := zerolog.Nop()
		return &nop
	}
	return deps.Log
}

var (
	// keyMu guards apiKey, the live copy of the configured key. The auth
	// middleware must never read the config struct directly: the key is written
	// at runtime by the RPC handlers and the management API.
	keyMu  sync.RWMutex
	apiKey string

	// enabled mirrors !config.MCPDisabled so the switch takes effect without a
	// restart.
	enabled atomic.Bool
)

// ErrNoAPIKey is returned when the server is enabled without a key. Enabling
// without one would mean either an unauthenticated HID control API or a port
// that is bound and rejects everything, so the switch is refused instead.
var ErrNoAPIKey = errors.New("an API key is required before the MCP server can be enabled")

var (
	// listenerMu guards the listener below. The server is started and stopped at
	// runtime by the management API, so its lifetime cannot be tied to a single
	// blocking call at boot.
	listenerMu   sync.Mutex
	listenerSrv  *http.Server
	listenerPort = DefaultPort
	listenerHTTP http.Handler
)

// ApplyAPIKey publishes the current key to the running server.
func ApplyAPIKey(key string) {
	keyMu.Lock()
	apiKey = key
	keyMu.Unlock()
	reconcileListener()
}

func currentAPIKey() string {
	keyMu.RLock()
	defer keyMu.RUnlock()
	return apiKey
}

// SetEnabled saves the switch and applies it to the running server: the port
// is opened when the server is enabled and a key is set, and closed otherwise.
func SetEnabled(value bool) error {
	if value && currentAPIKey() == "" {
		return ErrNoAPIKey
	}
	if err := deps.Key.SetEnabled(value); err != nil {
		return err
	}
	enabled.Store(value)
	reconcileListener()
	return nil
}

// PublishEnabled applies the saved switch without writing the config back. Used
// at startup, where the value already comes from the config file.
func PublishEnabled(value bool) {
	enabled.Store(value)
	reconcileListener()
}

// Start prepares the MCP server. In HTTP mode it returns immediately: the
// listener is opened by reconcileListener once the server is both enabled and
// has a key, and is closed again when either goes away. Stdio mode blocks on the
// transport because there is no listener to reconcile.
func Start(port int, stdio bool) {
	s := server.NewMCPServer("picokvm-mcp", "1.0.0")
	registerTools(s)

	if stdio {
		log().Info().Msg("Starting MCP stdio server")
		if err := server.ServeStdio(s); err != nil {
			log().Error().Err(err).Msg("MCP stdio server failed")
		}
		return
	}

	listenerMu.Lock()
	listenerPort = port
	listenerHTTP = newHTTPHandler(s)
	listenerMu.Unlock()

	if currentAPIKey() == "" {
		ApplyAPIKey(deps.Key.APIKey())
	}
	enabled.Store(deps.Key.Enabled())
	reconcileListener()
}

// reconcileListener brings the listener in line with the current switch and key.
// "Disabled" means the port is closed, not that it answers 503: a bound port is
// still visible to a scan.
func reconcileListener() {
	listenerMu.Lock()
	defer listenerMu.Unlock()

	if listenerHTTP == nil {
		// not started yet, Start will reconcile again once the handler exists
		return
	}

	wantListening := enabled.Load() && currentAPIKey() != ""

	switch {
	case wantListening && listenerSrv == nil:
		srv := &http.Server{
			Addr:              fmt.Sprintf(":%d", listenerPort),
			Handler:           listenerHTTP,
			ReadHeaderTimeout: 10 * time.Second,
		}
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			log().Error().Err(err).Str("addr", srv.Addr).Msg("MCP listener failed to bind")
			return
		}
		listenerSrv = srv
		log().Info().Str("addr", srv.Addr).Msg("MCP server listening")
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log().Error().Err(err).Msg("MCP server failed")
			}
		}()

	case !wantListening && listenerSrv != nil:
		srv := listenerSrv
		listenerSrv = nil
		// Close, not Shutdown: disabling has to release the port before this call
		// returns, otherwise a quick off/on would fail to bind, and an SSE stream
		// would keep Shutdown waiting for the length of its timeout anyway.
		if err := srv.Close(); err != nil {
			log().Warn().Err(err).Msg("MCP server close reported an error")
		}
		log().Info().Msg("MCP server stopped, port released")

	case wantListening && listenerSrv != nil:
		// already listening
	case !wantListening && listenerSrv == nil:
		if enabled.Load() && currentAPIKey() == "" {
			log().Warn().Msg("MCP server enabled but no API key configured, port stays closed")
		}
	}
}

// newHTTPHandler assembles the SSE endpoints with the guards layered around
// them. The order matters: the body limit is outermost so an oversized request
// is rejected before anything buffers it, CORS sits outside auth because a
// browser preflight carries no credentials, and auth is innermost so a rejected
// request never reaches the tool handlers.
func newHTTPHandler(s *server.MCPServer) http.Handler {
	sseServer := server.NewSSEServer(s)
	mux := http.NewServeMux()
	mux.Handle("/sse", sseServer.SSEHandler())
	mux.Handle("/message", sseServer.MessageHandler())

	var handler http.Handler = mux
	handler = withAuth(handler)
	handler = withCORS(handler)
	handler = withBodyLimit(handler)
	return handler
}

// ConfigView is the public view of the server settings. The key itself is never
// returned, only whether one is set plus a masked form.
func ConfigView() gin.H {
	key := currentAPIKey()

	listenerMu.Lock()
	listening := listenerSrv != nil
	port := listenerPort
	listenerMu.Unlock()

	return gin.H{
		"enabled":   enabled.Load(),
		"listening": listening,
		"apiKeySet": key != "",
		"apiKey":    MaskAPIKey(key),
		"port":      port,
	}
}
