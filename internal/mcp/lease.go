package mcp

import (
	"fmt"
	"sync"
	"time"
)

// Input lease.
//
// Four places can write to the HID gadget at the same time: the Web UI over the
// RPC channel, the local CLI API, the web console and the MCP tools. The mouse
// reports are stateless, so two writers only fight over the cursor position. The
// keyboard is not: a typed string is a sequence of hundreds of reports, and a
// keystroke from another writer landing inside that sequence scrambles the text
// on the remote host, while two writers holding different modifiers leaves keys
// stuck down.
//
// The lease serialises exactly that: every writer of a keyboard-class report
// takes it first, and a writer that finds it taken gets a clear error instead of
// corrupting the other session.
const (
	// OwnerWeb is the browser RPC channel.
	OwnerWeb = "web"
	// OwnerCLI is the local unix socket API.
	OwnerCLI = "cli"
)

// OwnerTool names the lease an MCP tool takes.
func OwnerTool(tool string) string {
	return "mcp:" + tool
}

// HoldForText estimates how long typing a string of n runes takes, so a long
// string is not cut off half way by its own lease.
func HoldForText(n int, delay time.Duration) time.Duration {
	hold := time.Duration(n)*(delay+15*time.Millisecond) + 5*time.Second
	if hold > maxInputHold {
		return maxInputHold
	}
	return hold
}

type inputLease struct {
	mu    sync.Mutex
	owner string
	until time.Time
	depth int
}

var lease inputLease

// AcquireInput takes the input lease for owner. The returned function releases
// it and is safe to call more than once. Acquiring while already holding it
// under the same owner nests instead of deadlocking, which is what lets a tool
// take the lease once and then call several primitives.
func AcquireInput(owner string, hold time.Duration) (func(), error) {
	if hold <= 0 {
		hold = defaultInputHold
	}
	if hold > maxInputHold {
		hold = maxInputHold
	}

	now := time.Now()

	lease.mu.Lock()
	if lease.owner != "" && lease.owner != owner && now.Before(lease.until) {
		busyFor := lease.until.Sub(now).Round(100 * time.Millisecond)
		previous := lease.owner
		lease.mu.Unlock()
		return nil, fmt.Errorf("input busy: %s is using the keyboard for another %s", previous, busyFor)
	}

	tookOver := lease.owner != "" && lease.owner != owner
	tookOverFrom := lease.owner
	if lease.owner == owner {
		lease.depth++
	} else {
		lease.owner = owner
		lease.depth = 1
	}
	lease.until = now.Add(hold)
	lease.mu.Unlock()

	if tookOver {
		// the previous holder died or was cut off, so it never got to release its
		// keys. Release everything before the new writer touches the keyboard.
		log().Warn().
			Str("previousOwner", tookOverFrom).
			Str("owner", owner).
			Msg("input lease expired, taking over and releasing held keys")
		if deps.Input != nil {
			if err := deps.Input.ReleaseKeys(); err != nil {
				log().Warn().Err(err).Msg("failed to release keys after taking over the input lease")
			}
		}
	}

	var once sync.Once
	return func() {
		once.Do(func() { lease.release(owner) })
	}, nil
}

func (l *inputLease) release(owner string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != owner {
		return
	}
	l.depth--
	if l.depth > 0 {
		return
	}
	l.owner = ""
	l.depth = 0
	l.until = time.Time{}
}

// InputHolder reports who currently holds the lease, for diagnostics.
func InputHolder() (owner string, remaining time.Duration, busy bool) {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.owner == "" {
		return "", 0, false
	}
	remaining = time.Until(lease.until)
	if remaining < 0 {
		remaining = 0
	}
	return lease.owner, remaining, true
}
