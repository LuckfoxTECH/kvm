package kvm

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Local web sessions.
//
// The device has a single account, and a single shared token meant every new
// login kicked the previous browser out while the token itself had to be
// persisted in the config file to survive a restart. Sessions are kept in
// memory instead: each login gets its own token, they expire on their own, and
// a restart simply asks everyone to log in again.
//
// Sessions are indexed by the SHA-256 of the token, so the tokens the browser
// holds are never stored verbatim in the process and no comparison of a secret
// is done at all: a lookup either finds the hash or it does not.

const (
	// localSessionTTL bounds how long a session stays valid after login.
	localSessionTTL = 24 * time.Hour
	// localSessionTokenBytes is the entropy of a session token.
	localSessionTokenBytes = 32
)

var (
	localSessionsMu sync.Mutex
	// localSessions maps SHA-256(token) -> expiry, so a stolen config file or a
	// core dump does not hand out live sessions.
	localSessions = make(map[string]time.Time)
)

// hashLocalSessionToken returns the map key for a token.
func hashLocalSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// dropExpiredSessionsLocked removes expired entries. Caller holds the mutex.
func dropExpiredSessionsLocked(now time.Time) {
	for key, expiresAt := range localSessions {
		if now.After(expiresAt) {
			delete(localSessions, key)
		}
	}
}

// newLocalSession creates a session and returns its token.
func newLocalSession() (string, error) {
	buf := make([]byte, localSessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	token := hex.EncodeToString(buf)
	now := time.Now()

	localSessionsMu.Lock()
	dropExpiredSessionsLocked(now)
	localSessions[hashLocalSessionToken(token)] = now.Add(localSessionTTL)
	localSessionsMu.Unlock()

	return token, nil
}

// validLocalSession reports whether a token belongs to a live session.
func validLocalSession(token string) bool {
	if token == "" {
		return false
	}

	now := time.Now()
	key := hashLocalSessionToken(token)

	localSessionsMu.Lock()
	defer localSessionsMu.Unlock()

	expiresAt, ok := localSessions[key]
	if !ok {
		return false
	}
	if now.After(expiresAt) {
		delete(localSessions, key)
		return false
	}
	return true
}

// dropLocalSession invalidates one session (logout).
func dropLocalSession(token string) {
	if token == "" {
		return
	}

	key := hashLocalSessionToken(token)
	localSessionsMu.Lock()
	delete(localSessions, key)
	localSessionsMu.Unlock()
}

// dropAllLocalSessions invalidates every session, used when the password
// changes: a password rotation has to log out anybody holding an old session.
func dropAllLocalSessions() {
	localSessionsMu.Lock()
	localSessions = make(map[string]time.Time)
	localSessionsMu.Unlock()
}

// setAuthCookie stores a session token in the browser. The cookie is httpOnly,
// Secure whenever the request came in over TLS, and SameSite=Lax so a page on
// another site cannot drive the device with the ambient cookie.
func setAuthCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("authToken", token, 0, "/", "", c.Request.TLS != nil, true)
}

// clearAuthCookie removes the session cookie from the browser.
func clearAuthCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("authToken", "", -1, "/", "", c.Request.TLS != nil, true)
}

// cookieValue returns a cookie value, or "" when the cookie is absent.
func cookieValue(c *gin.Context, name string) string {
	value, err := c.Cookie(name)
	if err != nil {
		return ""
	}
	return value
}
