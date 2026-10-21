package kvm

import (
	"sync"
	"time"
)

type RateLimitInfo struct {
	Failures       int
	BlockUntil     time.Time
	PenaltySeconds int
	LastSeen       time.Time
}

var (
	ipRateLimits   = make(map[string]*RateLimitInfo)
	ipRateLimitsMu sync.Mutex
)

const (
	MaxFailures      = 5
	BasePenalty      = 10 * 60 // 10 minutes in seconds
	CleanupInterval  = 1 * time.Hour
	RecordExpiration = 24 * time.Hour
)

func init() {
	go func() {
		for {
			time.Sleep(CleanupInterval)
			cleanupRateLimits()
		}
	}()
}

func cleanupRateLimits() {
	ipRateLimitsMu.Lock()
	defer ipRateLimitsMu.Unlock()

	now := time.Now()
	for ip, info := range ipRateLimits {
		if now.Sub(info.LastSeen) > RecordExpiration {
			delete(ipRateLimits, ip)
		}
	}
}

// CheckRateLimit checks if the IP is allowed to attempt login.
// Returns allowed (bool) and waitDuration (time.Duration) if blocked.
func CheckRateLimit(ip string) (bool, time.Duration) {
	ipRateLimitsMu.Lock()
	defer ipRateLimitsMu.Unlock()

	info, exists := ipRateLimits[ip]
	if !exists {
		return true, 0
	}

	if time.Now().Before(info.BlockUntil) {
		return false, info.BlockUntil.Sub(time.Now())
	}

	return true, 0
}

// RecordFailure records a failed login attempt for the IP.
func RecordFailure(ip string) {
	ipRateLimitsMu.Lock()
	defer ipRateLimitsMu.Unlock()

	info, exists := ipRateLimits[ip]
	if !exists {
		info = &RateLimitInfo{
			PenaltySeconds: BasePenalty,
		}
		ipRateLimits[ip] = info
	}

	info.LastSeen = time.Now()
	info.Failures++

	if info.Failures >= MaxFailures {
		info.BlockUntil = time.Now().Add(time.Duration(info.PenaltySeconds) * time.Second)

		// Extend penalty for next time. Doubling the penalty.
		info.PenaltySeconds *= 2
	}
}

// RecordSuccess resets the rate limit for the IP.
func RecordSuccess(ip string) {
	ipRateLimitsMu.Lock()
	defer ipRateLimitsMu.Unlock()
	delete(ipRateLimits, ip)
}

/*
 * Device-wide failure budget.
 *
 * The device has exactly one account, so a per-client-IP limit alone does not
 * bound online password guessing: a client can rotate source addresses, and a
 * forwarding header is client-controlled metadata we no longer trust. The total
 * number of failures across all clients is therefore capped as well, with a
 * penalty that grows on every lockout and resets on a successful login. This
 * keeps the guessing bound independent of anything an attacker can set.
 */
const (
	// MaxGlobalFailures is the device-wide failure budget.
	MaxGlobalFailures = 10
	// BaseGlobalPenalty is the first device-wide lockout.
	BaseGlobalPenalty = 30 * time.Second
	// MaxGlobalPenalty caps the escalation.
	MaxGlobalPenalty = 15 * time.Minute
)

var (
	globalRateLimitMu     sync.Mutex
	globalFailures        int
	globalBlockUntil      time.Time
	globalPenaltyDuration = BaseGlobalPenalty
)

// CheckGlobalRateLimit reports whether authentication is allowed device-wide.
func CheckGlobalRateLimit() (bool, time.Duration) {
	globalRateLimitMu.Lock()
	defer globalRateLimitMu.Unlock()

	if time.Now().Before(globalBlockUntil) {
		return false, time.Until(globalBlockUntil)
	}
	return true, 0
}

// RecordGlobalFailure counts one failed authentication, locking the device-wide
// budget and escalating the penalty once the budget is spent.
func RecordGlobalFailure() {
	globalRateLimitMu.Lock()
	defer globalRateLimitMu.Unlock()

	globalFailures++
	if globalFailures < MaxGlobalFailures {
		return
	}

	globalBlockUntil = time.Now().Add(globalPenaltyDuration)
	globalFailures = 0
	globalPenaltyDuration *= 2
	if globalPenaltyDuration > MaxGlobalPenalty {
		globalPenaltyDuration = MaxGlobalPenalty
	}
}

// RecordGlobalSuccess clears the device-wide budget after a valid login.
func RecordGlobalSuccess() {
	globalRateLimitMu.Lock()
	defer globalRateLimitMu.Unlock()

	globalFailures = 0
	globalPenaltyDuration = BaseGlobalPenalty
}
