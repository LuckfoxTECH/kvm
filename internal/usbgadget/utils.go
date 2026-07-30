package usbgadget

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

func joinPath(basePath string, paths []string) string {
	pathArr := append([]string{basePath}, paths...)
	return filepath.Join(pathArr...)
}

func hexToDecimal(hex string) (int64, error) {
	decimal, err := strconv.ParseInt(hex, 16, 64)
	if err != nil {
		return 0, err
	}
	return decimal, nil
}

func decimalToOctal(decimal int64) string {
	return fmt.Sprintf("%04o", decimal)
}

func hexToOctal(hex string) (string, error) {
	hex = strings.ToLower(hex)
	hex = strings.Replace(hex, "0x", "", 1) //remove 0x or 0X

	decimal, err := hexToDecimal(hex)
	if err != nil {
		return "", err
	}

	// Convert the decimal integer to an octal string.
	octal := decimalToOctal(decimal)
	return octal, nil
}

func compareFileContent(oldContent []byte, newContent []byte, looserMatch bool) bool {
	if bytes.Equal(oldContent, newContent) {
		return true
	}

	if len(oldContent) == len(newContent)+1 &&
		bytes.Equal(oldContent[:len(newContent)], newContent) &&
		oldContent[len(newContent)] == 10 {
		return true
	}

	if len(newContent) == 4 {
		if len(oldContent) < 6 || len(oldContent) > 7 {
			return false
		}

		if len(oldContent) == 7 && oldContent[6] == 0x0a {
			oldContent = oldContent[:6]
		}

		oldOctalValue, err := hexToOctal(string(oldContent))
		if err != nil {
			return false
		}

		if oldOctalValue == string(newContent) {
			return true
		}
	}

	if looserMatch {
		oldContentStr := strings.TrimSpace(string(oldContent))
		newContentStr := strings.TrimSpace(string(newContent))

		return oldContentStr == newContentStr
	}

	return false
}

func (u *UsbGadget) logWithSupression(counterName string, every int, logger *zerolog.Logger, err error, msg string, args ...interface{}) {
	u.logLock.Lock()
	defer u.logLock.Unlock()

	if _, ok := u.logSuppressionCounter[counterName]; !ok {
		u.logSuppressionCounter[counterName] = 0
	} else {
		u.logSuppressionCounter[counterName]++
	}

	l := logger.With().Int("counter", u.logSuppressionCounter[counterName]).Logger()

	if u.logSuppressionCounter[counterName]%every == 0 {
		if err != nil {
			l.Error().Err(err).Msgf(msg, args...)
		} else {
			l.Error().Msgf(msg, args...)
		}
	}
}

func (u *UsbGadget) resetLogSuppressionCounter(counterName string) {
	if _, ok := u.logSuppressionCounter[counterName]; !ok {
		u.logSuppressionCounter[counterName] = 0
	}
}

const hidWriteTimeout = 50 * time.Millisecond

// maxHidReportBacklog caps how many callers may queue behind one HID device
// lock. A stalled host makes every write cost hidWriteTimeout, so pointer events
// arrive far faster than they retire and each waiter pins a goroutine stack.
const maxHidReportBacklog = 8

// errHidReportDropped is returned when a HID report was discarded because the
// device already has maxHidReportBacklog reports queued.
var errHidReportDropped = errors.New("hid report dropped: device is not draining reports")

// enterHidBacklog reserves a slot in front of a HID device lock, returning false
// if the backlog is full so the caller drops the report instead of blocking.
// Buttons and absolute position are re-asserted by the next report; relative
// motion is genuinely lost, which is the trade against unbounded growth.
func enterHidBacklog(backlog *atomic.Int32) bool {
	if backlog.Add(1) > maxHidReportBacklog {
		backlog.Add(-1)
		return false
	}
	return true
}

func (u *UsbGadget) writeWithTimeout(file *os.File, data []byte) (n int, err error) {
	if err := file.SetWriteDeadline(time.Now().Add(hidWriteTimeout)); err != nil {
		return -1, err
	}

	n, err = file.Write(data)
	if err == nil {
		return
	}

	u.log.Trace().
		Str("file", file.Name()).
		Bytes("data", data).
		Err(err).
		Msg("write failed")

	if errors.Is(err, os.ErrDeadlineExceeded) {
		u.logWithSupression(
			fmt.Sprintf("writeWithTimeout_%s", file.Name()),
			1000,
			u.log,
			err,
			"write timed out: %s",
			file.Name(),
		)
		err = nil
	}

	return
}
