package kvm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Screenshot storage.
//
// The native JPEG encoder always writes the same spool file and emits a single
// ready signal, and its target path cannot be parameterized. The Go side
// therefore treats that file as a spool: every capture is copied to its own
// timestamped file, so concurrent captures cannot delete each other's file and
// the history is kept for later inspection.

const (
	// snapshotDir holds every screenshot.
	snapshotDir = "/userdata/picokvm/screenshot"

	// snapshotSpoolPath is the fixed path owned by the native encoder.
	snapshotSpoolPath = snapshotDir + "/kvm_screenshot.jpg"

	// snapshotKeep is how many timestamped screenshots are kept on disk.
	snapshotKeep = 20
)

// snapshotMu serializes captures. The native encoder has one spool file and one
// ready channel, so two concurrent captures would delete each other's file and
// steal each other's signal: one of them then falls back to polling and waits
// seconds for nothing.
var snapshotMu sync.Mutex

// newSnapshotPath returns a unique path inside snapshotDir.
func newSnapshotPath() (string, error) {
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create snapshot directory: %w", err)
	}
	// nanoseconds keep two captures inside the same second apart
	name := fmt.Sprintf("kvm_screenshot_%s.jpg", time.Now().Format("20060102_150405.000000000"))
	return filepath.Join(snapshotDir, name), nil
}

// persistSnapshot stores one capture under its own file name and returns the
// path, so a caller can report it back instead of only handing out bytes.
func persistSnapshot(data []byte) (string, error) {
	path, err := newSnapshotPath()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
	}
	pruneSnapshots()
	return path, nil
}

// pruneSnapshots keeps only the newest snapshotKeep timestamped screenshots.
func pruneSnapshots() {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return
	}

	type snapshotFile struct {
		path    string
		modTime time.Time
	}
	files := make([]snapshotFile, 0, len(entries))
	for _, entry := range entries {
		// the spool file is "kvm_screenshot.jpg", it does not match the prefix
		if entry.IsDir() ||
			!strings.HasPrefix(entry.Name(), "kvm_screenshot_") ||
			!strings.HasSuffix(entry.Name(), ".jpg") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, snapshotFile{
			path:    filepath.Join(snapshotDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	if len(files) <= snapshotKeep {
		return
	}

	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	for _, old := range files[snapshotKeep:] {
		if err := os.Remove(old.path); err != nil {
			logger.Warn().Err(err).Str("path", old.path).Msg("failed to prune old screenshot")
		}
	}
}
