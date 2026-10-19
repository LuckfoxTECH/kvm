package usbgadget

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"
)

var keyboardConfig = gadgetConfigItem{
	order:      1000,
	device:     "hid.usb0",
	path:       []string{"functions", "hid.usb0"},
	configPath: []string{"hid.usb0"},
	attrs: gadgetAttributes{
		"protocol":      "1",
		"subclass":      "1",
		"report_length": "8",
	},
	reportDesc: keyboardReportDesc,
}

// Source: https://www.kernel.org/doc/Documentation/usb/gadget_hid.txt
var keyboardReportDesc = []byte{
	0x05, 0x01, /* USAGE_PAGE (Generic Desktop)	          */
	0x09, 0x06, /* USAGE (Keyboard)                       */
	0xa1, 0x01, /* COLLECTION (Application)               */
	0x05, 0x07, /*   USAGE_PAGE (Keyboard)                */
	0x19, 0xe0, /*   USAGE_MINIMUM (Keyboard LeftControl) */
	0x29, 0xe7, /*   USAGE_MAXIMUM (Keyboard Right GUI)   */
	0x15, 0x00, /*   LOGICAL_MINIMUM (0)                  */
	0x25, 0x01, /*   LOGICAL_MAXIMUM (1)                  */
	0x75, 0x01, /*   REPORT_SIZE (1)                      */
	0x95, 0x08, /*   REPORT_COUNT (8)                     */
	0x81, 0x02, /*   INPUT (Data,Var,Abs)                 */
	0x95, 0x01, /*   REPORT_COUNT (1)                     */
	0x75, 0x08, /*   REPORT_SIZE (8)                      */
	0x81, 0x03, /*   INPUT (Cnst,Var,Abs)                 */
	0x95, 0x05, /*   REPORT_COUNT (5)                     */
	0x75, 0x01, /*   REPORT_SIZE (1)                      */

	0x05, 0x08, /*   USAGE_PAGE (LEDs)                    */
	0x19, 0x01, /*   USAGE_MINIMUM (Num Lock)             */
	0x29, 0x05, /*   USAGE_MAXIMUM (Kana)                 */
	0x91, 0x02, /*   OUTPUT (Data,Var,Abs)                */
	0x95, 0x01, /*   REPORT_COUNT (1)                     */
	0x75, 0x03, /*   REPORT_SIZE (3)                      */
	0x91, 0x03, /*   OUTPUT (Cnst,Var,Abs)                */
	0x95, 0x06, /*   REPORT_COUNT (6)                     */
	0x75, 0x08, /*   REPORT_SIZE (8)                      */
	0x15, 0x00, /*   LOGICAL_MINIMUM (0)                  */
	0x25, 0x65, /*   LOGICAL_MAXIMUM (101)                */
	0x05, 0x07, /*   USAGE_PAGE (Keyboard)                */
	0x19, 0x00, /*   USAGE_MINIMUM (Reserved)             */
	0x29, 0x65, /*   USAGE_MAXIMUM (Keyboard Application) */
	0x81, 0x00, /*   INPUT (Data,Ary,Abs)                 */
	0xc0, /* END_COLLECTION                         */
}

const (
	hidReadBufferSize = 8
	// https://www.usb.org/sites/default/files/documents/hid1_11.pdf
	// https://www.usb.org/sites/default/files/hut1_2.pdf
	KeyboardLedMaskNumLock    = 1 << 0
	KeyboardLedMaskCapsLock   = 1 << 1
	KeyboardLedMaskScrollLock = 1 << 2
	KeyboardLedMaskCompose    = 1 << 3
	KeyboardLedMaskKana       = 1 << 4
	ValidKeyboardLedMasks     = KeyboardLedMaskNumLock | KeyboardLedMaskCapsLock | KeyboardLedMaskScrollLock | KeyboardLedMaskCompose | KeyboardLedMaskKana
)

// Synchronization between LED states and CAPS LOCK, NUM LOCK, SCROLL LOCK,
// COMPOSE, and KANA events is maintained by the host and NOT the keyboard. If
// using the keyboard descriptor in Appendix B, LED states are set by sending a
// 5-bit absolute report to the keyboard via a Set_Report(Output) request.
type KeyboardState struct {
	NumLock    bool `json:"num_lock"`
	CapsLock   bool `json:"caps_lock"`
	ScrollLock bool `json:"scroll_lock"`
	Compose    bool `json:"compose"`
	Kana       bool `json:"kana"`
}

func getKeyboardState(b byte) KeyboardState {
	// should we check if it's the correct usage page?
	return KeyboardState{
		NumLock:    b&KeyboardLedMaskNumLock != 0,
		CapsLock:   b&KeyboardLedMaskCapsLock != 0,
		ScrollLock: b&KeyboardLedMaskScrollLock != 0,
		Compose:    b&KeyboardLedMaskCompose != 0,
		Kana:       b&KeyboardLedMaskKana != 0,
	}
}

func (u *UsbGadget) updateKeyboardState(b byte) {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	if b&^ValidKeyboardLedMasks != 0 {
		u.log.Trace().Uint8("b", b).Msg("contains invalid bits, ignoring")
		return
	}

	newState := getKeyboardState(b)
	if reflect.DeepEqual(u.keyboardState, newState) {
		return
	}
	u.log.Info().Interface("old", u.keyboardState).Interface("new", newState).Msg("keyboardState updated")
	u.keyboardState = newState

	if u.onKeyboardStateChange != nil {
		(*u.onKeyboardStateChange)(newState)
	}
}

func (u *UsbGadget) SetOnKeyboardStateChange(f func(state KeyboardState)) {
	u.onKeyboardStateChange = &f
}

func (u *UsbGadget) SetOnHidDeviceMissing(f func(device string, err error)) {
	u.onHidDeviceMissing = &f
}

func (u *UsbGadget) GetKeyboardState() KeyboardState {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	return u.keyboardState
}

func (u *UsbGadget) listenKeyboardEvents(ctx context.Context, file *os.File) {
	var path string
	if file != nil {
		path = file.Name()
	}
	l := u.log.With().Str("listener", "keyboardEvents").Str("path", path).Logger()
	l.Trace().Msg("starting")

	go func() {
		buf := make([]byte, hidReadBufferSize)
		for {
			select {
			case <-ctx.Done():
				l.Info().Msg("context done")
				return
			default:
				l.Trace().Msg("reading from keyboard")
				if file == nil {
					u.logWithSupression("keyboardHidFileNil", 100, &l, nil, "keyboardHidFile is nil")
					// show the error every 100 times to avoid spamming the logs
					time.Sleep(time.Second)
					continue
				}
				// reset the counter
				u.resetLogSuppressionCounter("keyboardHidFileNil")

				n, err := file.Read(buf)
				if err != nil {
					if ctx.Err() != nil {
						l.Info().Msg("context canceled while reading keyboard HID file")
						return
					}

					u.logWithSupression("keyboardHidFileRead", 100, &l, err, "failed to read")
					if reopenErr := u.reopenKeyboardHidFile(); reopenErr != nil {
						u.logWithSupression("keyboardHidFileReopen", 100, &l, reopenErr, "failed to reopen keyboard HID file")
					} else {
						u.resetLogSuppressionCounter("keyboardHidFileReopen")
					}
					return
				}
				u.resetLogSuppressionCounter("keyboardHidFileRead")

				l.Trace().Int("n", n).Bytes("buf", buf).Msg("got data from keyboard")
				if n < 1 {
					l.Info().Int("n", n).Msg("expected at least 1 byte, got 0")
					continue
				}
				u.updateKeyboardState(buf[0])
			}
		}
	}()
}

func openWithTimeout(name string, flag int, perm os.FileMode, timeout time.Duration) (*os.File, error) {
	type result struct {
		file *os.File
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := os.OpenFile(name, flag, perm)
		ch <- result{f, err}
	}()

	select {
	case r := <-ch:
		return r.file, r.err
	case <-time.After(timeout):
		// Drain the channel in the background to close the leaked fd if the
		// open eventually succeeds.
		go func() {
			if r := <-ch; r.file != nil {
				r.file.Close()
			}
		}()
		return nil, fmt.Errorf("open %s: timed out after %s", name, timeout)
	}
}

func (u *UsbGadget) closeKeyboardHidFileLocked() {
	if u.keyboardStateCancel != nil {
		u.keyboardStateCancel()
		u.keyboardStateCancel = nil
	}

	if u.keyboardHidFile != nil {
		u.keyboardHidFile.Close()
		u.keyboardHidFile = nil
	}
}

func (u *UsbGadget) openKeyboardHidFileLocked(forceReopen bool) error {
	if forceReopen {
		u.closeKeyboardHidFileLocked()
	} else if u.keyboardHidFile != nil {
		return nil
	}

	file, err := openWithTimeout("/dev/hidg0", os.O_RDWR, 0666, 3*time.Second)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "no such device") {
			u.log.Error().
				Str("device", "hidg0").
				Str("device_name", "keyboard").
				Err(err).
				Msg("HID device file missing, gadget may need reinitialization")
			if u.onHidDeviceMissing != nil {
				(*u.onHidDeviceMissing)("keyboard", err)
			}
		}
		return fmt.Errorf("failed to open hidg0: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	u.keyboardHidFile = file
	u.keyboardStateCtx = ctx
	u.keyboardStateCancel = cancel
	u.listenKeyboardEvents(ctx, file)

	return nil
}

func (u *UsbGadget) openKeyboardHidFile() error {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	return u.openKeyboardHidFileLocked(false)
}

func (u *UsbGadget) reopenKeyboardHidFile() error {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	return u.openKeyboardHidFileLocked(true)
}

func (u *UsbGadget) OpenKeyboardHidFile() error {
	return u.openKeyboardHidFile()
}

func (u *UsbGadget) ReopenKeyboardHidFile() error {
	return u.reopenKeyboardHidFile()
}

// keyboardWriteAttempts bounds how long a keyboard report waits for the host,
// at hidWriteTimeout per attempt, before it is dropped.
const keyboardWriteAttempts = 10

func (u *UsbGadget) keyboardWriteHidFileLocked(modifier byte, keys []byte) error {
	if len(keys) > 6 {
		keys = keys[:6]
	}
	if len(keys) < 6 {
		keys = append(keys, make([]byte, 6-len(keys))...)
	}

	data := []byte{modifier, 0, keys[0], keys[1], keys[2], keys[3], keys[4], keys[5]}

	if u.keyboardHidFile == nil {
		if err := u.openKeyboardHidFileLocked(false); err != nil {
			return err
		}
	}

	// A report can't be written until the host has collected the previous
	// one. Dropping it loses key state, and a dropped release leaves the key
	// held on the host, which auto-repeats it, so retry before giving up.
	var err error
	for attempt := 0; attempt < keyboardWriteAttempts; attempt++ {
		_, err = u.writeWithTimeout(u.keyboardHidFile, data)
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			break
		}
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		// The host has stopped collecting reports. Drop this one, as before,
		// rather than reopen the HID file.
		return nil
	}
	if err != nil {
		u.logWithSupression("keyboardWriteHidFile", 100, u.log, err, "failed to write to hidg0")
		u.closeKeyboardHidFileLocked()
		return err
	}
	u.resetLogSuppressionCounter("keyboardWriteHidFile")

	u.resetUserInputTime()
	return nil
}

type autoReleaseTimer struct {
	timer  *time.Timer
	key    byte
	active bool
}

type KeysDownState struct {
	Modifier byte
	Keys     [6]byte
}

func (u *UsbGadget) scheduleAutoRelease(key byte) {
	// Cancel existing timer for this key
	for i := range u.autoReleaseTimers {
		if u.autoReleaseTimers[i].key == key && u.autoReleaseTimers[i].active {
			u.autoReleaseTimers[i].timer.Stop()
			u.autoReleaseTimers[i].active = false
		}
	}

	// Schedule new timer
	timer := time.AfterFunc(100*time.Millisecond, func() {
		u.autoReleaseKey(key)
	})

	u.autoReleaseTimers = append(u.autoReleaseTimers, autoReleaseTimer{
		timer:  timer,
		key:    key,
		active: true,
	})
}

func (u *UsbGadget) autoReleaseKey(key byte) {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	// Remove key from buffer
	found := false
	for i := 0; i < len(u.keysDownState.Keys); i++ {
		if u.keysDownState.Keys[i] == key {
			found = true
		}
		if found && i < len(u.keysDownState.Keys)-1 {
			u.keysDownState.Keys[i] = u.keysDownState.Keys[i+1]
		}
	}
	if found {
		u.keysDownState.Keys[len(u.keysDownState.Keys)-1] = 0
		u.keyboardWriteHidFileLocked(u.keysDownState.Modifier, u.keysDownState.Keys[:])
	}

	// Mark timer as inactive
	for i := range u.autoReleaseTimers {
		if u.autoReleaseTimers[i].key == key && u.autoReleaseTimers[i].active {
			u.autoReleaseTimers[i].active = false
		}
	}
}

func (u *UsbGadget) cancelAutoRelease(key byte) {
	for i := range u.autoReleaseTimers {
		if u.autoReleaseTimers[i].key == key && u.autoReleaseTimers[i].active {
			u.autoReleaseTimers[i].timer.Stop()
			u.autoReleaseTimers[i].active = false
		}
	}
}

func (u *UsbGadget) resetAllAutoReleaseTimers() {
	for i := range u.autoReleaseTimers {
		if u.autoReleaseTimers[i].active {
			u.autoReleaseTimers[i].timer.Stop()
			u.autoReleaseTimers[i].active = false
		}
	}
}

func (u *UsbGadget) KeypressReport(key byte, press bool) error {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	if press {
		// Check if key already in buffer
		for _, k := range u.keysDownState.Keys {
			if k == key {
				return nil // Already pressed
			}
		}

		// Find empty slot
		emptySlot := -1
		for i, k := range u.keysDownState.Keys {
			if k == 0 {
				emptySlot = i
				break
			}
		}

		if emptySlot == -1 {
			// Buffer full - ErrorRollOver
			u.keysDownState.Keys = [6]byte{0x01, 0x01, 0x01, 0x01, 0x01, 0x01}
		} else {
			u.keysDownState.Keys[emptySlot] = key
		}

		u.scheduleAutoRelease(key)
	} else {
		// Remove key from buffer
		found := false
		for i := 0; i < len(u.keysDownState.Keys); i++ {
			if u.keysDownState.Keys[i] == key {
				found = true
			}
			if found && i < len(u.keysDownState.Keys)-1 {
				u.keysDownState.Keys[i] = u.keysDownState.Keys[i+1]
			}
		}
		if found {
			u.keysDownState.Keys[len(u.keysDownState.Keys)-1] = 0
		}

		u.cancelAutoRelease(key)
	}

	return u.keyboardWriteHidFileLocked(u.keysDownState.Modifier, u.keysDownState.Keys[:])
}

func (u *UsbGadget) KeypressKeepAlive() error {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	// Reset auto-release timers for all currently held keys
	for _, key := range u.keysDownState.Keys {
		if key != 0 {
			u.scheduleAutoRelease(key)
		}
	}

	return nil
}

func (u *UsbGadget) KeyboardReport(modifier uint8, keys []uint8) error {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	u.keysDownState.Modifier = modifier
	copy(u.keysDownState.Keys[:], keys)

	return u.keyboardWriteHidFileLocked(modifier, keys)
}
