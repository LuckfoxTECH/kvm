package mcp

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// withBodyLimit caps the body of every MCP request. The body is buffered so an
// oversized request is answered with a real 413 instead of the generic 400 the
// SSE handler returns when it fails to decode a truncated body.
func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBytes+1))
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		if len(body) > MaxRequestBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		next.ServeHTTP(w, r)
	})
}

// withCORS answers same-origin browsers only. MCP clients are not browsers and
// send no Origin header, so nothing here needs a wildcard: a page hosted on
// another site must not be able to drive HID with a key it managed to read.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if !isSameOrigin(origin, r.Host) {
				writeJSONError(w, http.StatusForbidden, "cross-origin request rejected")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

// withAuth guards every MCP request with the API key.
//
// There is deliberately no loopback exemption: the port is reachable from the
// network, and a reverse proxy on this device would make any remote attacker
// look like a local caller. The key is the only credential, and an unprovisioned
// key denies every request.
func withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !enabled.Load() {
			writeJSONError(w, http.StatusServiceUnavailable, "MCP server is disabled")
			return
		}
		expected := currentAPIKey()
		if expected == "" {
			writeJSONError(w, http.StatusUnauthorized, "MCP API key not configured")
			return
		}
		key, ok := ParseBearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
			return
		}
		// constant-time comparison, the key is a secret
		if subtle.ConstantTimeCompare([]byte(key), []byte(expected)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "invalid api key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ParseBearerToken extracts the token of an "Authorization: Bearer <token>"
// header. The scheme is matched case-insensitively as RFC 7235 requires, and the
// token must be a single word, so "Bearer abc def" is rejected instead of being
// truncated at the first space the way fmt.Sscanf("%s") would.
func ParseBearerToken(header string) (string, bool) {
	const scheme = "bearer "
	if len(header) < len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	token := strings.TrimSpace(header[len(scheme):])
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":%q}`, message)
}

// requireSameOrigin rejects a request a browser sends from another site. A
// non-browser client sends no Origin header and is let through: it carries no
// ambient browser credentials, so it is not a CSRF vector.
func requireSameOrigin(c *gin.Context) {
	origin := c.GetHeader("Origin")
	if origin == "" {
		c.Next()
		return
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || !strings.EqualFold(u.Host, c.Request.Host) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "cross-origin request rejected"})
		return
	}
	c.Next()
}
