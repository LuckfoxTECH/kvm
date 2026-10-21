package kvm

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/pprof"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"kvm/internal/logging"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	gin_logger "github.com/gin-contrib/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"kvm/internal/mcp"
)

//nolint:typecheck
//go:embed all:static
var staticFiles embed.FS

type WebRTCSessionRequest struct {
	Sd         string   `json:"sd"`
	OidcGoogle string   `json:"OidcGoogle,omitempty"`
	IP         string   `json:"ip,omitempty"`
	ICEServers []string `json:"iceServers,omitempty"`
}

type SetPasswordRequest struct {
	Password string `json:"password"`
}

type LoginRequest struct {
	Password string `json:"password"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

type LocalDevice struct {
	AuthMode     *string `json:"authMode"`
	DeviceID     string  `json:"deviceId"`
	LoopbackOnly bool    `json:"loopbackOnly"`
}

type DeviceStatus struct {
	IsSetup bool `json:"isSetup"`
}

type SetupRequest struct {
	LocalAuthMode string `json:"localAuthMode"`
	Password      string `json:"password,omitempty"`
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	gin.DisableConsoleColor()
	r := gin.Default()

	// Do not trust any forwarding header: gin otherwise believes X-Forwarded-For
	// from any peer, which would let a client pick its own ClientIP() -- that is
	// what the login rate limiter buckets on, and what the audit log records.
	// If the device is ever put behind a reverse proxy, narrow this to that
	// proxy's address instead of trusting everyone.
	_ = r.SetTrustedProxies(nil)

	r.Use(limitRequestBody(maxRequestBodyBytes))
	r.Use(gin_logger.SetLogger(
		gin_logger.WithLogger(func(*gin.Context, zerolog.Logger) zerolog.Logger {
			return *ginLogger
		}),
	))
	staticFS, _ := fs.Sub(staticFiles, "static")

	// Add a custom middleware to set cache headers for images
	// This is crucial for optimizing the initial welcome screen load time
	// By enabling caching, we ensure that pre-loaded images are stored in the browser cache
	// This allows for a smoother enter animation and improved user experience on the welcome screen
	r.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")

		if strings.HasPrefix(c.Request.URL.Path, "/static/") {
			ext := filepath.Ext(c.Request.URL.Path)
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" {
				c.Header("Cache-Control", "public, max-age=300") // Cache for 5 minutes
				c.Header("Pragma", "")
				c.Header("Expires", "")
			}
		}
		c.Next()
	})

	r.StaticFS("/static", http.FS(staticFS))
	r.POST("/auth/login-local", handleLogin)

	// MCP management: enable/disable the MCP server and rotate its API key
	mcp.RegisterManagementRoutes(r, protectedMiddleware())

	// We use this to determine if the device is setup
	r.GET("/device/status", handleDeviceStatus)

	// We use this to provide the UI with the device configuration
	//r.GET("/device/ui-config.js", handleDeviceUIConfig)

	// We use this to setup the device in the welcome page
	r.POST("/device/setup", handleSetup)

	// Debug endpoints (pprof + Prometheus metrics) are registered here but gated per
	// request by devModeOnlyMiddleware: they answer 404 unless developer mode is on.
	debugRouter := r.Group("/", devModeOnlyMiddleware())
	{
		// pprof. Durations are capped because one long CPU profile pegs the single
		// Cortex-A7 core and makes the device unresponsive.
		debugRouter.GET("/debug/pprof/", gin.WrapF(pprof.Index))
		debugRouter.GET("/debug/pprof/cmdline", gin.WrapF(pprof.Cmdline))
		debugRouter.GET("/debug/pprof/profile", clampProfileSeconds(pprof.Profile, maxProfileSeconds))
		debugRouter.POST("/debug/pprof/symbol", gin.WrapF(pprof.Symbol))
		debugRouter.GET("/debug/pprof/symbol", gin.WrapF(pprof.Symbol))
		debugRouter.GET("/debug/pprof/trace", clampProfileSeconds(pprof.Trace, maxProfileSeconds))
		debugRouter.GET("/debug/pprof/allocs", gin.WrapH(pprof.Handler("allocs")))
		debugRouter.GET("/debug/pprof/block", gin.WrapH(pprof.Handler("block")))
		debugRouter.GET("/debug/pprof/goroutine", gin.WrapH(pprof.Handler("goroutine")))
		debugRouter.GET("/debug/pprof/heap", gin.WrapH(pprof.Handler("heap")))
		debugRouter.GET("/debug/pprof/mutex", gin.WrapH(pprof.Handler("mutex")))
		debugRouter.GET("/debug/pprof/threadcreate", gin.WrapH(pprof.Handler("threadcreate")))

		// A Prometheus metrics endpoint.
		debugRouter.GET("/metrics", gin.WrapH(promhttp.Handler()))
	}

	// Developer mode protected routes
	developerModeRouter := r.Group("/developer/")
	developerModeRouter.Use(basicAuthProtectedMiddleware(true))
	{
		logging.AttachSSEHandler(developerModeRouter)
	}

	// Protected routes (allows both password and noPassword modes)
	protected := r.Group("/")
	protected.Use(protectedMiddleware())
	{
		/*
		 * Legacy WebRTC session endpoint
		 *
		 * This endpoint is maintained for backward compatibility when users upgrade from a version
		 * using the legacy HTTP-based signaling method to the new WebSocket-based signaling method.
		 *
		 * During the upgrade process, when the "Rebooting device after update..." message appears,
		 * the browser still runs the previous JavaScript code which polls this endpoint to establish
		 * a new WebRTC session. Once the session is established, the page will automatically reload
		 * with the updated code.
		 *
		 * Without this endpoint, the stale JavaScript would fail to establish a connection,
		 * causing users to see the "Rebooting device after update..." message indefinitely
		 * until they manually refresh the page, leading to a confusing user experience.
		 */
		protected.POST("/webrtc/session", handleWebRTCSession)
		protected.GET("/webrtc/signaling/client", handleLocalWebRTCSignal)
		protected.GET("/device", handleDevice)
		protected.POST("/auth/logout", handleLogout)

		protected.POST("/auth/password-local", handleCreatePassword)
		protected.PUT("/auth/password-local", handleUpdatePassword)
		protected.DELETE("/auth/local-password", handleDeletePassword)
		protected.POST("/storage/upload", handleUploadHttp)
		protected.POST("/ota/upload", handleOTAUploadHttp)
		protected.POST("/ota/upload-local-pkg", handleLocalPackageUploadHttp)
		protected.GET("/storage/download", handleDownloadHttp)
		protected.GET("/storage/sd-download", handleSDDownloadHttp)
		protected.POST("/api/rpc", handleRpcRequest)
		protected.GET("/api/ice-servers", handleGetIceServers)
		protected.GET("/terminal/ws", handleTerminalWS)
		protected.GET("/serial/ws", handleSerialWS)
		protected.GET("/video/stream", handleVideoStream)
	}

	// Catch-all route for SPA
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method == "GET" && c.NegotiateFormat(gin.MIMEHTML) == gin.MIMEHTML {
			c.FileFromFS("/", http.FS(staticFS))
			return
		}
		c.Status(http.StatusNotFound)
	})

	return r
}

var currentSession *Session

var (
	currentHTTPSessionID    string
	httpSessionToNotify     string
	httpSessionMu           sync.Mutex
	httpSessionLockVersion  int64
	httpSessionSeenVersions map[string]int64
	invalidHTTPSessions     map[string]bool
)

func handleWebRTCSession(c *gin.Context) {
	var req WebRTCSessionRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := newSession(SessionConfig{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	sd, err := session.ExchangeOffer(req.Sd)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}
	if currentSession != nil {
		writeJSONRPCEvent("otherSessionConnected", nil, currentSession)
		peerConn := currentSession.peerConnection
		go func() {
			time.Sleep(1 * time.Second)
			_ = peerConn.Close()
		}()
	}
	currentSession = session
	c.JSON(http.StatusOK, gin.H{"sd": sd})
}

var (
	pingMessage = []byte("ping")
	pongMessage = []byte("pong")
)

// WebsocketPingInterval is the interval at which the websocket client sends ping messages
const WebsocketPingInterval = 15 * time.Second

var (
	metricConnectionLastPingTimestamp = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kvm_connection_last_ping_timestamp_seconds",
			Help: "The timestamp when the last ping response was received",
		},
		[]string{"type", "source"},
	)
	metricConnectionLastPingReceivedTimestamp = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kvm_connection_last_ping_received_timestamp_seconds",
			Help: "The timestamp when the last ping request was received",
		},
		[]string{"type", "source"},
	)
	metricConnectionLastPingDuration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kvm_connection_last_ping_duration_seconds",
			Help: "The duration of the last ping response",
		},
		[]string{"type", "source"},
	)
	metricConnectionPingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "kvm_connection_ping_duration_seconds",
			Help: "The duration of the ping response",
			Buckets: []float64{
				0.1, 0.5, 1, 10,
			},
		},
		[]string{"type", "source"},
	)
	metricConnectionTotalPingSentCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kvm_connection_ping_sent_total",
			Help: "The total number of pings sent to the connection",
		},
		[]string{"type", "source"},
	)
	metricConnectionTotalPingReceivedCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kvm_connection_ping_received_total",
			Help: "The total number of pings received from the connection",
		},
		[]string{"type", "source"},
	)
	metricConnectionSessionRequestCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kvm_connection_session_requests_total",
			Help: "The total number of session requests received",
		},
		[]string{"type", "source"},
	)
	metricConnectionSessionRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "kvm_connection_session_request_duration_seconds",
			Help: "The duration of session requests",
			Buckets: []float64{
				0.1, 0.5, 1, 10,
			},
		},
		[]string{"type", "source"},
	)
	metricConnectionLastSessionRequestTimestamp = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kvm_connection_last_session_request_timestamp_seconds",
			Help: "The timestamp of the last session request",
		},
		[]string{"type", "source"},
	)
	metricConnectionLastSessionRequestDuration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kvm_connection_last_session_request_duration",
			Help: "The duration of the last session request",
		},
		[]string{"type", "source"},
	)
)

func handleLocalWebRTCSignal(c *gin.Context) {
	// get the source from the request
	source := c.ClientIP()

	connectionID := c.Query("id")
	if connectionID == "" {
		connectionID = uuid.New().String()
	}

	scopedLogger := websocketLogger.With().
		Str("component", "websocket").
		Str("source", source).
		Str("sourceType", "local").
		Str("connectionID", connectionID).
		Logger()

	scopedLogger.Info().Msg("new websocket connection established")

	// Create WebSocket options with InsecureSkipVerify to bypass origin check
	wsOptions := &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow connections from any origin
		OnPingReceived: func(ctx context.Context, payload []byte) bool {
			scopedLogger.Debug().Bytes("payload", payload).Msg("ping frame received")

			metricConnectionTotalPingReceivedCount.WithLabelValues("local", source).Inc()
			metricConnectionLastPingReceivedTimestamp.WithLabelValues("local", source).SetToCurrentTime()

			return true
		},
	}

	wsCon, err := websocket.Accept(c.Writer, c.Request, wsOptions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Now use conn for websocket operations
	defer wsCon.Close(websocket.StatusNormalClosure, "")

	err = wsjson.Write(context.Background(), wsCon, gin.H{
		"type": "device-metadata",
		"data": gin.H{"deviceVersion": builtAppVersion, "connectionID": connectionID},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	err = handleWebRTCSignalWsMessages(wsCon, source, connectionID, &scopedLogger)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}

func handleWebRTCSignalWsMessages(
	wsCon *websocket.Conn,
	source string,
	connectionID string,
	scopedLogger *zerolog.Logger,
) error {
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer func() {
		cancelRun()
	}()

	// connection type
	var sourceType = "local"

	l := scopedLogger.With().
		Str("source", source).
		Str("sourceType", sourceType).
		Str("connectionID", connectionID).
		Logger()

	l.Info().Msg("new websocket connection established")

	go func() {
		for {
			time.Sleep(WebsocketPingInterval)

			if ctxErr := runCtx.Err(); ctxErr != nil {
				if !errors.Is(ctxErr, context.Canceled) {
					l.Warn().Str("error", ctxErr.Error()).Msg("websocket connection closed")
				} else {
					l.Trace().Str("error", ctxErr.Error()).Msg("websocket connection closed as the context was canceled")
				}
				return
			}

			// set the timer for the ping duration
			timer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
				metricConnectionLastPingDuration.WithLabelValues(sourceType, source).Set(v)
				metricConnectionPingDuration.WithLabelValues(sourceType, source).Observe(v)
			}))

			l.Trace().Msg("sending ping frame")
			err := wsCon.Ping(runCtx)

			if err != nil {
				l.Warn().Str("error", err.Error()).Msg("websocket ping error")
				cancelRun()
				return
			}

			// dont use `defer` here because we want to observe the duration of the ping
			duration := timer.ObserveDuration()

			metricConnectionTotalPingSentCount.WithLabelValues(sourceType, source).Inc()
			metricConnectionLastPingTimestamp.WithLabelValues(sourceType, source).SetToCurrentTime()

			l.Trace().Str("duration", duration.String()).Msg("received pong frame")
		}
	}()

	for {
		typ, msg, err := wsCon.Read(runCtx)
		if err != nil {
			l.Warn().Str("error", err.Error()).Msg("websocket read error")
			return err
		}
		if typ != websocket.MessageText {
			// ignore non-text messages
			continue
		}

		var message struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}

		if bytes.Equal(msg, pingMessage) {
			l.Info().Str("message", string(msg)).Msg("ping message received")
			err = wsCon.Write(context.Background(), websocket.MessageText, pongMessage)
			if err != nil {
				l.Warn().Str("error", err.Error()).Msg("unable to write pong message")
				return err
			}

			metricConnectionTotalPingReceivedCount.WithLabelValues(sourceType, source).Inc()
			metricConnectionLastPingReceivedTimestamp.WithLabelValues(sourceType, source).SetToCurrentTime()

			continue
		}

		err = json.Unmarshal(msg, &message)
		if err != nil {
			l.Warn().Str("error", err.Error()).Msg("unable to parse ws message")
			continue
		}

		if message.Type == "offer" {
			l.Info().Msg("new session request received")
			var req WebRTCSessionRequest
			err = json.Unmarshal(message.Data, &req)
			if err != nil {
				l.Warn().Str("error", err.Error()).Msg("unable to parse session request data")
				continue
			}

			if req.OidcGoogle != "" {
				l.Info().Str("oidcGoogle", req.OidcGoogle).Msg("new session request with OIDC Google")
			}

			metricConnectionSessionRequestCount.WithLabelValues(sourceType, source).Inc()
			metricConnectionLastSessionRequestTimestamp.WithLabelValues(sourceType, source).SetToCurrentTime()
			err = handleSessionRequest(runCtx, wsCon, req, source, &l)
			if err != nil {
				l.Warn().Str("error", err.Error()).Msg("error starting new session")
				continue
			}
		} else if message.Type == "new-ice-candidate" {
			l.Info().Str("data", string(message.Data)).Msg("The client sent us a new ICE candidate")
			var candidate webrtc.ICECandidateInit

			// Attempt to unmarshal as a ICECandidateInit
			if err := json.Unmarshal(message.Data, &candidate); err != nil {
				l.Warn().Str("error", err.Error()).Msg("unable to parse incoming ICE candidate data")
				continue
			}

			if candidate.Candidate == "" {
				l.Warn().Msg("empty incoming ICE candidate, skipping")
				continue
			}

			l.Info().Str("data", fmt.Sprintf("%v", candidate)).Msg("unmarshalled incoming ICE candidate")

			if currentSession == nil {
				l.Warn().Msg("no current session, skipping incoming ICE candidate")
				continue
			}

			l.Info().Str("data", fmt.Sprintf("%v", candidate)).Msg("adding incoming ICE candidate to current session")
			if err = currentSession.peerConnection.AddICECandidate(candidate); err != nil {
				l.Warn().Str("error", err.Error()).Msg("failed to add incoming ICE candidate to our peer connection")
			}
		}
	}
}

func handleLogin(c *gin.Context) {
	if config.LocalAuthMode == "noPassword" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Login is disabled in noPassword mode"})
		return
	}

	ip := c.ClientIP()
	if allowed, wait := CheckRateLimit(ip); !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("Too many failed attempts. Please try again in %s", wait.Round(time.Second)),
		})
		return
	}

	// the per-IP budget alone is not a bound on guessing, this is
	if allowed, wait := CheckGlobalRateLimit(); !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("Too many failed attempts on this device. Please try again in %s", wait.Round(time.Second)),
		})
		return
	}

	var req LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(req.Password))
	if err != nil {
		RecordFailure(ip)
		RecordGlobalFailure()
		// record the address the connection actually came from, the headers are
		// client-controlled metadata
		logger.Warn().Str("ip", ip).Str("remoteAddr", c.Request.RemoteAddr).Msg("local login failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	RecordSuccess(ip)
	RecordGlobalSuccess()

	token, err := newLocalSession()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	setAuthCookie(c, token)

	c.JSON(http.StatusOK, gin.H{"message": "Login successful"})
}

func handleLogout(c *gin.Context) {
	dropLocalSession(cookieValue(c, "authToken"))
	clearAuthCookie(c)
	c.JSON(http.StatusOK, gin.H{"message": "Logout successful"})
}

// maxProfileSeconds caps how long /debug/pprof/profile and /debug/pprof/trace may
// sample the CPU. The device has a single core, so a long profile makes it unusable.
const maxProfileSeconds = 10

// devModeOnlyMiddleware gates the debug endpoints (pprof, metrics). Developer mode
// must be enabled. Password-protected setups additionally require the WebUI session
// cookie, while noPassword setups are let through so the debug panel stays usable.
func devModeOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		devModeState, err := rpcGetDevModeState()
		if err != nil || !devModeState.Enabled {
			// Answer 404 so the endpoints do not advertise their existence.
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		if config.LocalAuthMode != "noPassword" {
			if !validLocalSession(cookieValue(c, "authToken")) {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
		}

		c.Next()
	}
}

// clampProfileSeconds rewrites the seconds query parameter so a single request cannot
// sample the CPU for longer than maxSeconds. pprof defaults to 30s when the parameter
// is missing, so the default has to be clamped as well.
func clampProfileSeconds(handler http.HandlerFunc, maxSeconds int) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Request.URL.Query()
		seconds := maxSeconds
		if raw := query.Get("seconds"); raw != "" {
			if requested, err := strconv.Atoi(raw); err == nil && requested > 0 && requested < seconds {
				seconds = requested
			}
		}
		query.Set("seconds", strconv.Itoa(seconds))
		c.Request.URL.RawQuery = query.Encode()

		handler(c.Writer, c.Request)
	}
}

func protectedMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.LocalAuthMode == "noPassword" {
			c.Next()
			return
		}

		authToken := cookieValue(c, "authToken")
		if !validLocalSession(authToken) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func sendErrorJsonThenAbort(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
	c.Abort()
}

func basicAuthProtectedMiddleware(requireDeveloperMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if requireDeveloperMode {
			devModeState, err := rpcGetDevModeState()
			if err != nil {
				sendErrorJsonThenAbort(c, http.StatusInternalServerError, "Failed to get developer mode state")
				return
			}

			if !devModeState.Enabled {
				sendErrorJsonThenAbort(c, http.StatusUnauthorized, "Developer mode is not enabled")
				return
			}
		}

		if config.LocalAuthMode == "noPassword" {
			sendErrorJsonThenAbort(c, http.StatusForbidden, "The resource is not available in noPassword mode")
			return
		}

		// calculate basic auth credentials
		_, password, ok := c.Request.BasicAuth()
		if !ok {
			c.Header("WWW-Authenticate", "Basic realm=\"KVM\"")
			sendErrorJsonThenAbort(c, http.StatusUnauthorized, "Basic auth is required")
			return
		}

		ip := c.ClientIP()
		if allowed, wait := CheckRateLimit(ip); !allowed {
			sendErrorJsonThenAbort(c, http.StatusTooManyRequests, fmt.Sprintf("Too many failed attempts. Please try again in %s", wait.Round(time.Second)))
			return
		}
		if allowed, wait := CheckGlobalRateLimit(); !allowed {
			sendErrorJsonThenAbort(c, http.StatusTooManyRequests, fmt.Sprintf("Too many failed attempts on this device. Please try again in %s", wait.Round(time.Second)))
			return
		}

		err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(password))
		if err != nil {
			RecordFailure(ip)
			RecordGlobalFailure()
			sendErrorJsonThenAbort(c, http.StatusUnauthorized, "Invalid password")
			return
		}

		RecordSuccess(ip)

		c.Next()
	}
}

func RunWebServer() {
	r := setupRouter()

	// Determine the binding address based on the config
	bindAddress := ":80" // Default to all interfaces
	if config.LocalLoopbackOnly {
		bindAddress = "localhost:80" // Loopback only (both IPv4 and IPv6)
	}

	logger.Info().Str("bindAddress", bindAddress).Bool("loopbackOnly", config.LocalLoopbackOnly).Msg("Starting web server")
	err := r.Run(bindAddress)
	if err != nil {
		panic(err)
	}
}

func handleDevice(c *gin.Context) {
	response := LocalDevice{
		AuthMode:     &config.LocalAuthMode,
		DeviceID:     GetDeviceID(),
		LoopbackOnly: config.LocalLoopbackOnly,
	}

	c.JSON(http.StatusOK, response)
}

func handleCreatePassword(c *gin.Context) {
	if config.HashedPassword != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password already set"})
		return
	}

	// We only allow users with noPassword mode to set a new password
	// Users with password mode are not allowed to set a new password without providing the old password
	// We have a PUT endpoint for changing the password, use that instead
	if config.LocalAuthMode != "noPassword" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password mode is not enabled"})
		return
	}

	var req SetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	config.HashedPassword = string(hashedPassword)
	config.LocalAuthToken = ""
	config.LocalAuthMode = "password"
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	token, err := newLocalSession()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}
	setAuthCookie(c, token)

	c.JSON(http.StatusCreated, gin.H{"message": "Password set successfully"})
}

func handleUpdatePassword(c *gin.Context) {
	if config.HashedPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password is not set"})
		return
	}

	// We only allow users with password mode to change their password
	// Users with noPassword mode are not allowed to change their password
	if config.LocalAuthMode != "password" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password mode is not enabled"})
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.OldPassword == "" || req.NewPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Incorrect old password"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash new password"})
		return
	}

	config.HashedPassword = string(hashedPassword)
	config.LocalAuthToken = ""
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	// rotating the password must log out every session opened with the old one
	dropAllLocalSessions()

	token, err := newLocalSession()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}
	setAuthCookie(c, token)

	c.JSON(http.StatusOK, gin.H{"message": "Password updated successfully"})
}

func handleDeletePassword(c *gin.Context) {
	if config.HashedPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password is not set"})
		return
	}

	if config.LocalAuthMode != "password" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password mode is not enabled"})
		return
	}

	var req LoginRequest // Reusing LoginRequest struct for password
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.HashedPassword), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Incorrect password"})
		return
	}

	// Disable password
	config.HashedPassword = ""
	config.LocalAuthToken = ""
	config.LocalAuthMode = "noPassword"
	if err := SaveConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration"})
		return
	}

	dropAllLocalSessions()
	clearAuthCookie(c)

	c.JSON(http.StatusOK, gin.H{"message": "Password disabled successfully"})
}

func handleDeviceStatus(c *gin.Context) {
	response := DeviceStatus{
		IsSetup: config.LocalAuthMode != "",
	}

	c.JSON(http.StatusOK, response)
}

func handleDeviceUIConfig(c *gin.Context) {
	config, _ := json.Marshal(gin.H{
		"DEVICE_VERSION": builtAppVersion,
	})
	if config == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal config"})
		return
	}

	response := fmt.Sprintf("window.KVM_CONFIG = %s;", config)

	c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(response))
}

func handleSetup(c *gin.Context) {
	// Check if the device is already set up
	if config.LocalAuthMode != "" || config.HashedPassword != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Device is already set up"})
		return
	}

	var req SetupRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LocalAuthMode != "password" && req.LocalAuthMode != "noPassword" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid localAuthMode"})
		return
	}

	config.LocalAuthMode = req.LocalAuthMode

	if req.LocalAuthMode == "password" {
		if req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password is required for password mode"})
			return
		}

		// Hash the password
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}

		config.HashedPassword = string(hashedPassword)
		config.LocalAuthToken = ""
	} else {
		// For noPassword mode, ensure the password field is empty
		config.HashedPassword = ""
		config.LocalAuthToken = ""
	}

	err := SaveConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save config"})
		return
	}

	if config.LocalAuthMode == "password" {
		token, tokenErr := newLocalSession()
		if tokenErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
			return
		}
		setAuthCookie(c, token)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device setup completed successfully"})
}

func handleRpcRequest(c *gin.Context) {
	var req JSONRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if isBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON RPC request"})
		return
	}
	sessionID := c.GetHeader("X-Session-ID")
	if sessionID == "" {
		var err error
		sessionID, err = c.Cookie("httpSessionId")
		if err != nil || sessionID == "" {
			sessionID = uuid.New().String()
			c.SetCookie("httpSessionId", sessionID, 7*24*60*60, "/", "", false, false)
		}
	}

	var event *JSONRPCEvent

	httpSessionMu.Lock()
	if httpSessionSeenVersions == nil {
		httpSessionSeenVersions = make(map[string]int64)
	}

	if invalidHTTPSessions == nil {
		invalidHTTPSessions = make(map[string]bool)
	}

	invalid := false

	if req.Method == "confirmOtherSession" {
		httpSessionLockVersion++
		currentHTTPSessionID = sessionID
		httpSessionToNotify = ""
		httpSessionSeenVersions[sessionID] = httpSessionLockVersion

		for id := range httpSessionSeenVersions {
			if id == sessionID {
				delete(invalidHTTPSessions, id)
				continue
			}
			invalidHTTPSessions[id] = true
		}

		//logger.Info().
		//	Str("sessionID", sessionID).
		//	Str("currentHTTPSessionID", currentHTTPSessionID).
		//	Str("httpSessionToNotify", httpSessionToNotify).
		//	Int64("httpSessionLockVersion", httpSessionLockVersion).
		//	Str("rpcMethod", req.Method).
		//	Msg("handleRpcRequest confirmOtherSession")
	} else {
		if invalidHTTPSessions[sessionID] {
			invalid = true
			event = &JSONRPCEvent{
				JSONRPC: "2.0",
				Method:  "sessionInvalidated",
			}
		} else {
			if _, ok := httpSessionSeenVersions[sessionID]; !ok {
				httpSessionSeenVersions[sessionID] = httpSessionLockVersion
			}

			//logger.Info().
			//	Str("sessionID", sessionID).
			//	Str("currentHTTPSessionID", currentHTTPSessionID).
			//	Str("httpSessionToNotify", httpSessionToNotify).
			//	Int64("httpSessionLockVersion", httpSessionLockVersion).
			//	Str("rpcMethod", req.Method).
			//	Msg("handleRpcRequest before session check")

			if currentHTTPSessionID == "" {
				currentHTTPSessionID = sessionID
			} else if sessionID == httpSessionToNotify {
				event = &JSONRPCEvent{
					JSONRPC: "2.0",
					Method:  "otherSessionConnected",
				}
				httpSessionToNotify = ""
			} else if sessionID != currentHTTPSessionID {
				if version, ok := httpSessionSeenVersions[sessionID]; ok && version >= httpSessionLockVersion {
					httpSessionToNotify = currentHTTPSessionID
					currentHTTPSessionID = sessionID
				}
			}

			//logger.Info().
			//	Str("sessionID", sessionID).
			//	Str("currentHTTPSessionID", currentHTTPSessionID).
			//	Str("httpSessionToNotify", httpSessionToNotify).
			//	Int64("httpSessionLockVersion", httpSessionLockVersion).
			//	Bool("hasEvent", event != nil).
			//	Str("rpcMethod", req.Method).
			//	Msg("handleRpcRequest after session check")
		}
	}
	httpSessionMu.Unlock()

	if invalid {
		if isJSONRPCNotification(req) {
			c.Status(http.StatusNoContent)
			return
		}

		response := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: map[string]interface{}{
				"code":    -32001,
				"message": "Session invalidated",
			},
			ID: req.ID,
		}

		if event != nil {
			c.JSON(http.StatusOK, gin.H{
				"response": response,
				"event":    event,
			})
			return
		}

		c.JSON(http.StatusOK, response)
		return
	}

	response, _ := DispatchRPCRequest(req)

	if isJSONRPCNotification(req) {
		c.Status(http.StatusNoContent)
		return
	}

	if event != nil {
		c.JSON(http.StatusOK, gin.H{
			"response": response,
			"event":    event,
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

func handleGetIceServers(c *gin.Context) {
	LoadConfig()
	servers, err := rpcGetIceServers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"iceServers": servers})
}

func handleVideoStream(c *gin.Context) {
	logger.Info().Msg("HTTP video stream request received")

	c.Header("Content-Type", "video/x-h264")
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	// Flush headers immediately so client knows the connection is established
	c.Writer.Flush()
	logger.Info().Msg("HTTP video stream headers flushed, waiting for video data")

	id, ch := videoBroadcaster.Subscribe()
	logger.Info().Str("subscriber_id", id).Msg("subscribed to video broadcaster")
	defer func() {
		videoBroadcaster.Unsubscribe(id)
		logger.Info().Str("subscriber_id", id).Msg("unsubscribed from video broadcaster")
	}()

	ctx := c.Request.Context()
	frameCount := 0

	for {
		select {
		case frame, ok := <-ch:
			if !ok {
				logger.Info().Int("total_frames", frameCount).Msg("video broadcaster channel closed")
				return
			}
			data := frame.Data()
			frameCount++
			if frameCount == 1 {
				logger.Info().Int("size", len(data)).Msg("first video frame received")
			}
			if _, err := c.Writer.Write(data); err != nil {
				logger.Warn().Err(err).Int("total_frames", frameCount).Msg("error writing video data")
				frame.Release()
				return
			}
			c.Writer.Flush()
			frame.Release()
		case <-ctx.Done():
			logger.Info().Int("total_frames", frameCount).Msg("client disconnected")
			return
		}
	}
}

func handleSessionRequest(
	ctx context.Context,
	c *websocket.Conn,
	req WebRTCSessionRequest,
	source string,
	scopedLogger *zerolog.Logger,
) error {
	var sourceType = "local"

	timer := prometheus.NewTimer(
		prometheus.ObserverFunc(func(v float64) {
			metricConnectionLastSessionRequestDuration.WithLabelValues(sourceType, source).Set(v)
			metricConnectionSessionRequestDuration.WithLabelValues(sourceType, source).Observe(v)
		}),
	)
	defer timer.ObserveDuration()

	session, err := newSession(SessionConfig{
		ws:         c,
		LocalIP:    req.IP,
		ICEServers: req.ICEServers,
		Logger:     scopedLogger,
	})
	if err != nil {
		_ = wsjson.Write(context.Background(), c, gin.H{"error": err})
		return err
	}

	sd, err := session.ExchangeOffer(req.Sd)
	if err != nil {
		_ = wsjson.Write(context.Background(), c, gin.H{"error": err})
		return err
	}
	if currentSession != nil {
		writeJSONRPCEvent("otherSessionConnected", nil, currentSession)
		peerConn := currentSession.peerConnection
		go func() {
			time.Sleep(1 * time.Second)
			_ = peerConn.Close()
		}()
	}

	logger.Info().Interface("session", session).Msg("new session accepted")
	logger.Trace().Interface("session", session).Msg("new session accepted")
	currentSession = session
	_ = wsjson.Write(context.Background(), c, gin.H{"type": "answer", "data": sd})
	return nil
}

// maxRequestBodyBytes caps how much of a request body the server will read.
// Every JSON control request is a few KiB; the largest legitimate JSON payload
// is a base64 storage paste of a few hundred KiB, so 1 MiB is generous. Without
// a cap a client can make the device buffer an arbitrary amount of memory, and
// the oversized body is rejected before authentication runs so it costs no
// cycles.
const maxRequestBodyBytes = 1 << 20

// maxUploadBytes caps the streaming uploads. Their handlers write straight to
// disk instead of buffering, so the cap only bounds how much a single session can
// put on the filesystem, not memory. Firmware images are tens of MiB.
const maxUploadBytes = 512 << 20

// uploadRoutes stream their bodies to disk and therefore get the upload cap
// instead of the JSON one. Keyed by gin's route pattern.
var uploadRoutes = map[string]int64{
	"/storage/upload":       maxUploadBytes,
	"/ota/upload":           maxUploadBytes,
	"/ota/upload-local-pkg": maxUploadBytes,
}

// limitRequestBody rejects oversized requests and hard-caps the reader so a
// body sent without Content-Length (chunked) is bounded as well.
func limitRequestBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := limit
		if routeLimit, ok := uploadRoutes[c.FullPath()]; ok {
			limit = routeLimit
		}

		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

// isBodyTooLarge reports whether a bind error was caused by the body limit
// rather than by malformed input, so the client gets 413 instead of 400.
func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
