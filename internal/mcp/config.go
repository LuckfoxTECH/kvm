package mcp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterManagementRoutes mounts the MCP management endpoints on the web
// server, so the Web UI can toggle the server and rotate the key. The key is
// never returned by the read endpoint: only whether one is set, plus a masked
// form.
//
// Write endpoints are guarded twice: loginGuard requires the login session
// cookie in password mode (the same guard the other protected web handlers
// use), and requireSameOrigin blocks a browser page hosted on another site from
// performing the call (CSRF).
func RegisterManagementRoutes(r *gin.Engine, loginGuard gin.HandlerFunc) {
	r.GET("/mcp/config", loginGuard, handleGetConfig)
	r.POST("/mcp/config", requireSameOrigin, loginGuard, handleSetConfig)
	r.POST("/mcp/key/regenerate", requireSameOrigin, loginGuard, handleRegenerateKey)
}

// MaskAPIKey keeps the recognizable prefix and the last four characters.
func MaskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 12 {
		return "***"
	}
	return key[:8] + "..." + key[len(key)-4:]
}

func handleGetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, ConfigView())
}

func handleSetConfig(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "enabled (bool) is required"})
		return
	}
	if err := SetEnabled(*req.Enabled); err != nil {
		if errors.Is(err, ErrNoAPIKey) {
			// a client-side mistake, not a server fault: the UI has to ask for a
			// key first
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log().Info().Bool("enabled", *req.Enabled).Msg("MCP server toggled via management API")
	c.JSON(http.StatusOK, ConfigView())
}

// RegenerateKey creates a new API key, saves it and publishes it to the running
// server. The plaintext is returned exactly once.
func RegenerateKey() (string, error) {
	key, err := deps.Key.RegenerateKey()
	if err != nil {
		return "", err
	}
	ApplyAPIKey(key)
	return key, nil
}

func handleRegenerateKey(c *gin.Context) {
	key, err := RegenerateKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log().Info().Msg("MCP API key regenerated via management API")
	// the plaintext key is returned exactly once, the caller has to store it
	c.JSON(http.StatusOK, gin.H{"apiKey": key})
}
