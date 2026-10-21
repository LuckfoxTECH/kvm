package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Durable audit trail.
//
// The daemon log is a live view: it only exists while a developer has a console
// or the log SSE page open, its level can be raised, and it is not written to
// disk at all. A record of "who typed what" has to outlive that, so every tool
// call also appends one JSON line to AuditPath.
const (
	// auditMaxBytes is the size at which the trail is rotated to <path>.1.
	auditMaxBytes = 1 << 20 // 1 MiB
)

// auditEntry is one line of the trail.
type auditEntry struct {
	Time     string `json:"time"`
	Tool     string `json:"tool"`
	Args     string `json:"args,omitempty"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
	Duration string `json:"duration"`
}

var auditFile struct {
	mu   sync.Mutex
	path string
}

// appendAudit writes one entry to the audit file. A failure here must never
// break the tool call, so it is reported in the daemon log and dropped.
func appendAudit(entry auditEntry) {
	path := deps.AuditPath
	if path == "" {
		return
	}

	line, err := json.Marshal(entry)
	if err != nil {
		log().Warn().Err(err).Msg("failed to encode audit entry")
		return
	}
	line = append(line, '\n')

	auditFile.mu.Lock()
	defer auditFile.mu.Unlock()

	if auditFile.path != path {
		auditFile.path = path
	}

	if info, statErr := os.Stat(path); statErr == nil && info.Size() > auditMaxBytes {
		// keep exactly one previous file: an audit trail that fills the flash is
		// worse than a short one
		_ = os.Rename(path, path+".1")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log().Warn().Err(err).Str("path", path).Msg("failed to create audit directory")
		return
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		log().Warn().Err(err).Str("path", path).Msg("failed to open audit file")
		return
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Write(line); err != nil {
		log().Warn().Err(err).Str("path", path).Msg("failed to write audit entry")
	}
}

func auditTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func auditDuration(d time.Duration) string {
	return fmt.Sprintf("%.3fs", d.Seconds())
}
