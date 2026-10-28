package usbgadget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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
	reportDesc: keyboardReportDescDefault,
}

// Source: https://www.kernel.org/doc/Documentation/usb/gadget_hid.txt
//
// This is the descriptor every arrangement uses unless it is listed in
// keyboardFormFactorReportDescs. It declares the array range 0-101, which covers
// the ANSI and ISO arrangements but not the JIS IME keys at 0x87-0x91.
var keyboardReportDescDefault = []byte{
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

// keyboardReportDescWide is identical to keyboardReportDescDefault except for the
// two items that declare the array range:
//
//	LOGICAL_MAXIMUM 0x25 0x65 -> 0x27 0xe7 0x00 0x00 0x00 (231, four-byte item)
//	USAGE_MAXIMUM   0x29 0x65 -> 0x29 0xe7                (231, last defined usage)
//
// The JIS IME keys sit at usages 0x87-0x91, so both bounds have to reach past
// them or the host drops those usages. Keep the logical and usage ranges aligned.
// Use a four-byte maximum to keep it positive on macOS and make this descriptor
// 66 bytes for the Windows control-transfer length comparison. Its report format
// and range are unchanged from the 64-byte version; other arrangements retain
// the default descriptor.
var keyboardReportDescWide = []byte{
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
	0x27, 0xe7, 0x00, 0x00, 0x00, /* LOGICAL_MAXIMUM (231, four-byte) */
	0x05, 0x07, /*   USAGE_PAGE (Keyboard)                */
	0x19, 0x00, /*   USAGE_MINIMUM (Reserved)             */
	0x29, 0xe7, /*   USAGE_MAXIMUM (231 = Right GUI)      */
	0x81, 0x00, /*   INPUT (Data,Ary,Abs)                 */
	0xc0, /* END_COLLECTION                         */
}

// keyboardFormFactorReportDescs lists the physical arrangements that need a
// descriptor other than the default. Add an entry here to give a new arrangement
// its own array range; nothing else has to change, the USB side is picked up from
// this table.
var keyboardFormFactorReportDescs = map[string][]byte{
	"JIS": keyboardReportDescWide,
}

// KeyboardReportDescForFormFactor returns the HID report descriptor the physical
// keyboard arrangement needs.
func KeyboardReportDescForFormFactor(formFactor string) []byte {
	if desc, ok := keyboardFormFactorReportDescs[formFactor]; ok {
		return desc
	}
	return keyboardReportDescDefault
}

// ApplyKeyboardReportDescForFormFactor points the keyboard function at the
// descriptor the arrangement needs and reports whether that changed anything. The
// new descriptor only reaches the host after the gadget is rebuilt, because
// report_desc is read when the function is bound.
func ApplyKeyboardReportDescForFormFactor(formFactor string) bool {
	desc := KeyboardReportDescForFormFactor(formFactor)
	if bytes.Equal(keyboardConfig.reportDesc, desc) {
		return false
	}
	keyboardConfig.reportDesc = desc
	// defaultGadgetConfig holds a copy of keyboardConfig taken at package init, so
	// it has to be updated too or a recreated gadget would use the old descriptor.
	defaultGadgetConfig["keyboard"] = keyboardConfig
	return true
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
	// KeyboardLedMaskShift is bit 6 of the LED byte. The descriptor only declares
	// five LEDs, but the host writes a whole byte and lights this bit on some
	// platforms. Rejecting the report over that extra bit would drop the lock
	// LEDs carried in the same byte, and the host never resends them.
	KeyboardLedMaskShift  = 1 << 6
	ValidKeyboardLedMasks = KeyboardLedMaskNumLock | KeyboardLedMaskCapsLock | KeyboardLedMaskScrollLock | KeyboardLedMaskCompose | KeyboardLedMaskKana | KeyboardLedMaskShift
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
	// Shift is not part of the main USB HID spec.
	Shift bool `json:"shift"`

	// Reported is false until the host has sent at least one LED output report.
	// LED reports only travel host -> device and the host sends them only when a
	// lock light changes, so a freshly started process knows nothing until then.
	// Without this flag a client cannot tell "every light is off" apart from
	// "the host has never told us anything".
	Reported bool `json:"reported"`

	// raw is the original LED bitmap reported by the host. It is unexported so
	// the JSON-RPC wire format is unchanged; Byte() exposes it to hidrpc.
	raw byte
}

// Byte returns the raw LED bitmap of the keyboard state.
func (k *KeyboardState) Byte() byte {
	return k.raw
}

func getKeyboardState(b byte, reported bool) KeyboardState {
	// should we check if it's the correct usage page?
	return KeyboardState{
		NumLock:    b&KeyboardLedMaskNumLock != 0,
		CapsLock:   b&KeyboardLedMaskCapsLock != 0,
		ScrollLock: b&KeyboardLedMaskScrollLock != 0,
		Compose:    b&KeyboardLedMaskCompose != 0,
		Kana:       b&KeyboardLedMaskKana != 0,
		Shift:      b&KeyboardLedMaskShift != 0,
		raw:        b,
		Reported:   reported,
	}
}

func (u *UsbGadget) updateKeyboardState(b byte) {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	if b&^ValidKeyboardLedMasks != 0 {
		// Warn, not Trace: dropping the only LED report the host sends leaves
		// this process blind until the host re-enumerates, and a silent drop is
		// impossible to diagnose from the field.
		u.log.Warn().Uint8("b", b).Msg("keyboard LED report contains invalid bits, ignoring")
		return
	}

	newState := getKeyboardState(b, true)
	// A report carrying 0x00 is still a report: without the Reported check the
	// first all-off report would be swallowed as "no change" and clients would
	// keep showing the state as unknown.
	if u.keyboardState.raw == newState.raw && u.keyboardState.Reported {
		return
	}
	u.log.Trace().Uint8("old", u.keyboardState.raw).Uint8("new", newState.raw).Msg("keyboardState updated")
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
					u.logWithSuppression("keyboardHidFileNil", 100, &l, nil, "keyboardHidFile is nil")
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

					u.logWithSuppression("keyboardHidFileRead", 100, &l, err, "failed to read")
					if reopenErr := u.reopenKeyboardReader(ctx, file); reopenErr != nil {
						u.logWithSuppression("keyboardHidFileReopen", 100, &l, reopenErr, "failed to reopen keyboard HID file")
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
				u.keyboardLock.Lock()
				if ctx.Err() == nil && u.keyboardHidFile == file {
					u.updateKeyboardState(buf[0])
				}
				u.keyboardLock.Unlock()
			}
		}
	}()
}

func (u *UsbGadget) openWithTimeout(name string, flag int, perm os.FileMode, timeout time.Duration) (*os.File, error) {
	type result struct {
		file *os.File
		err  error
	}
	ch := make(chan result, 1)
	// Caller holds hidLifecycle.RLock, so a rebind cannot start waiting until
	// this admission is recorded. Ownership lasts through late-result cleanup.
	u.hidOpens.begin()
	go func() {
		f, err := os.OpenFile(name, flag, perm)
		ch <- result{f, err}
	}()

	select {
	case r := <-ch:
		u.hidOpens.end()
		return r.file, r.err
	case <-time.After(timeout):
		// Drain the channel in the background to close the leaked fd if the
		// open eventually succeeds.
		go func() {
			defer u.hidOpens.end()
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

// isMissingHidNodeError reports whether the open failed because the /dev/hidgN
// node is absent, as opposed to a permission, busy, or not-ready error.
func isMissingHidNodeError(err error) bool {
	if err == nil {
		return false
	}
	// ENXIO ("no such device or address") is the normal answer while the gadget
	// is being rebound: the node exists, the function behind it is not ready yet.
	// Its message contains "no such device", so a substring match would classify
	// it as a lost node - and with the UDC still reported as configured, that
	// looked like a genuine fault and triggered another rebuild, which produced
	// more ENXIO. Check the errno first so the transient state never enters the
	// missing-node path.
	if IsHIDTemporarilyUnavailableError(err) {
		return false
	}
	return errors.Is(err, os.ErrNotExist) ||
		strings.Contains(err.Error(), "no such file or directory")
}

// hidNodeGenuinelyMissing separates "the gadget lost the node" from "the node does
// not exist yet because the host has not finished enumerating".
//
// Only the first is a fault worth rebuilding the gadget for. The configfs node
// appears once the host has enumerated and configured the device, so a host that
// takes longer to load its drivers (Windows installs a HID stack per new device)
// reaches this with the UDC still in "default". Treating that window as a fault
// rebuilt the gadget, the rebuild restarted enumeration, and the same window came
// back - an endless device-reset loop with /dev/hidg0 missing the whole time.
func (u *UsbGadget) hidNodeGenuinelyMissing() bool {
	return strings.TrimSpace(u.GetUsbState(false)) == USBStateConfigured
}

func (u *UsbGadget) openKeyboardHidFileLocked(forceReopen bool) error {
	if !u.enabledDevices.Keyboard {
		// The keyboard function is switched off, so /dev/hidg0 does not exist. That
		// is the intended state, not a fault: report it as benign so the caller does
		// not log a missing device or ask for a gadget rebuild on every report.
		return ErrHIDDeviceNotConfigured
	}
	if forceReopen {
		u.closeKeyboardHidFileLocked()
	} else if u.keyboardHidFile != nil {
		openInfo, openErr := u.keyboardHidFile.Stat()
		nodeInfo, nodeErr := os.Stat(u.keyboardHidFile.Name())
		if openErr == nil && nodeErr == nil && os.SameFile(openInfo, nodeInfo) {
			return nil
		}
		u.closeKeyboardHidFileLocked()
	}

	hidNodePath := u.HidgNodePath("keyboard")
	file, err := u.openWithTimeout(hidNodePath, os.O_RDWR, 0666, 3*time.Second)
	if err != nil {
		if isMissingHidNodeError(err) {
			if u.hidNodeGenuinelyMissing() {
				u.log.Error().
					Str("device", hidNodePath).
					Str("device_name", "keyboard").
					Err(err).
					Msg("HID device file missing, gadget may need reinitialization")
				if u.onHidDeviceMissing != nil {
					(*u.onHidDeviceMissing)("keyboard", err)
				}
			} else {
				// Enumeration still in progress. A later report reopens the node on
				// demand, so staying quiet here is enough to let startup settle.
				u.log.Info().
					Str("device", hidNodePath).
					Err(err).
					Msg("hidg0 not present yet; host has not finished enumeration")
			}
		}
		return fmt.Errorf("failed to open %s: %w", hidNodePath, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	u.keyboardHidFile = file
	u.keyboardStateCtx = ctx
	u.keyboardStateCancel = cancel
	u.listenKeyboardEvents(ctx, file)

	return nil
}

func (u *UsbGadget) openKeyboardHidFile() error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	return u.openKeyboardHidFileLocked(false)
}

func (u *UsbGadget) reopenKeyboardHidFile() error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	return u.openKeyboardHidFileLocked(true)
}

func (u *UsbGadget) reopenKeyboardReader(ctx context.Context, file *os.File) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	// An error from a canceled generation must not close its replacement.
	if ctx.Err() != nil || u.keyboardHidFile != file {
		return nil
	}
	return u.openKeyboardHidFileLocked(true)
}

func (u *UsbGadget) OpenKeyboardHidFile() error {
	return u.openKeyboardHidFile()
}

func (u *UsbGadget) ReopenKeyboardHidFile() error {
	return u.reopenKeyboardHidFile()
}

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

	_, err := u.writeWithTimeout(u.keyboardHidFile, data)
	if err != nil {
		u.logWithSuppression("keyboardWriteHidFile", 100, u.log, err, "failed to write to hidg0")
		u.closeKeyboardHidFileLocked()
		return err
	}
	u.resetLogSuppressionCounter("keyboardWriteHidFile")

	u.resetUserInputTime()
	return nil
}

// KeysDownState is the authoritative keyboard state and the single source of
// truth for both the USB HID report and the client-side key highlighting.
// Left and right modifiers are distinct bits in the modifier byte
// (ModifierMaskLeftShift=0x02, ModifierMaskRightShift=0x20), so holding one
// side never implies the other.
// This logic is paralleled in the browser in useKeyboard.ts — keep in sync.
// KeysDownState is the device-truth keyboard state reported to clients.
// JSON tags matter: the legacy JSON-RPC fallback marshals this struct directly,
// and the frontend reads lowercase "modifier"/"keys". Keys stays a fixed
// [6]byte array so encoding/json emits a number array (a []byte would marshal
// as a base64 string).
type KeysDownState struct {
	Modifier byte    `json:"modifier"`
	Keys     [6]byte `json:"keys"`
}

func (u *UsbGadget) GetKeysDownState() KeysDownState {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	return u.keysDownState
}

// SetOnKeysDownChange registers the callback fired whenever the pressed-key set
// actually changes. usb.go forwards it to the active session so the client can
// highlight keys from device truth instead of guessing from what it sent.
func (u *UsbGadget) SetOnKeysDownChange(f func(state KeysDownState)) {
	u.keyboardStateLock.Lock()
	defer u.keyboardStateLock.Unlock()

	u.onKeysDownChange = &f
}

// UpdateKeysDown stores the new state and notifies the listener only when it
// changed, so a repeated keep-alive report does not spam the client.
func (u *UsbGadget) UpdateKeysDown(modifier byte, keys []byte) KeysDownState {
	var next [6]byte
	copy(next[:], keys)

	// Error roll over is a transient overflow marker, not real key state.
	if next[0] == hidErrorRollOver {
		next = [6]byte{}
	}

	state := KeysDownState{Modifier: modifier, Keys: next}

	u.keyboardStateLock.Lock()
	if u.keysDownState == state {
		u.keyboardStateLock.Unlock()
		return state
	}
	u.keysDownState = state
	onChange := u.onKeysDownChange
	u.keyboardStateLock.Unlock()

	if onChange != nil {
		(*onChange)(state)
	}

	return state
}

// DefaultAutoReleaseDuration is how long a held non-modifier key survives
// without a keep-alive before being released automatically.
const DefaultAutoReleaseDuration = 100 * time.Millisecond

type kbdAutoReleaseTimer struct {
	timer *time.Timer
}

// scheduleAutoRelease arms the auto-release timer for a non-modifier key.
// One map slot per key code: N held keys cost O(1) per schedule and the map
// never grows past the number of distinct keys, unlike the old slice append
// that added an entry on every keep-alive and scanned the whole slice each time.
func (u *UsbGadget) scheduleAutoRelease(key byte) {
	u.kbdAutoReleaseLock.Lock()
	defer unlockWithLog(&u.kbdAutoReleaseLock, u.log, "autoRelease scheduled")

	u.scheduleAutoReleaseLocked(key, DefaultAutoReleaseDuration)
}

// Replacing the entry invalidates callbacks that have already started, which
// Timer.Stop and Timer.Reset cannot cancel.
func (u *UsbGadget) scheduleAutoReleaseLocked(key byte, duration time.Duration) {
	if entry := u.kbdAutoReleaseTimers[key]; entry != nil {
		entry.timer.Stop()
	}
	entry := &kbdAutoReleaseTimer{}
	entry.timer = time.AfterFunc(duration, func() {
		u.performAutoRelease(key, entry)
	})
	u.kbdAutoReleaseTimers[key] = entry
}

func (u *UsbGadget) cancelAutoRelease(key byte) {
	u.kbdAutoReleaseLock.Lock()
	defer unlockWithLog(&u.kbdAutoReleaseLock, u.log, "autoRelease cancelled")

	if entry := u.kbdAutoReleaseTimers[key]; entry != nil {
		entry.timer.Stop()
		delete(u.kbdAutoReleaseTimers, key)
	}
}

// CancelAllAutoReleaseTimers stops and removes every pending auto-release
// timer. Call it when the keyboard state is forcibly cleared (session
// disconnect) so a racing timer cannot reintroduce a stale key.
func (u *UsbGadget) CancelAllAutoReleaseTimers() {
	u.kbdAutoReleaseLock.Lock()
	defer unlockWithLog(&u.kbdAutoReleaseLock, u.log, "all autoRelease timers cancelled")

	for key, entry := range u.kbdAutoReleaseTimers {
		if entry != nil {
			entry.timer.Stop()
		}
		delete(u.kbdAutoReleaseTimers, key)
	}
}

// DelayAutoReleaseWithDuration pushes every pending deadline out by
// resetDuration. Used by the keep-alive path so a client that is still holding
// keys keeps them held.
func (u *UsbGadget) DelayAutoReleaseWithDuration(resetDuration time.Duration) {
	u.kbdAutoReleaseLock.Lock()
	defer unlockWithLog(&u.kbdAutoReleaseLock, u.log, "autoRelease delayed")

	u.log.Debug().Dur("reset_duration", resetDuration).Msg("delaying auto-release with dynamic duration")

	for key, entry := range u.kbdAutoReleaseTimers {
		if entry != nil {
			u.scheduleAutoReleaseLocked(key, resetDuration)
		}
	}
}

func (u *UsbGadget) performAutoRelease(key byte, entry *kbdAutoReleaseTimer) {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	u.kbdAutoReleaseLock.Lock()

	if entry == nil || u.kbdAutoReleaseTimers[key] != entry {
		u.kbdAutoReleaseLock.Unlock()
		return
	}

	entry.timer.Stop()
	delete(u.kbdAutoReleaseTimers, key)
	u.kbdAutoReleaseLock.Unlock()

	// Timers are only ever armed for non-modifier keys, so a modifier can never
	// be released here. That is the fix for Shift being dropped while still
	// held for longer than the release window.
	state := u.GetKeysDownState()
	alreadyReleased := true
	for i := range state.Keys {
		if state.Keys[i] == key {
			alreadyReleased = false
			break
		}
	}
	if alreadyReleased {
		return
	}

	if _, err := u.keypressReportLocked(key, false); err != nil && !IsHIDBenignError(err) {
		u.log.Warn().Uint8("key", key).Msg("failed to release key")
	}
}

// keypressReport applies a single key transition and returns the resulting
// state. It parallels the kernel hid-gadget driver: modifiers live in the
// modifier byte, all other keys live in the fixed 6-slot key array.
// This logic is paralleled in the browser in useKeyboard.ts — keep in sync.
func (u *UsbGadget) keypressReport(key byte, press bool) (KeysDownState, error) {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	return u.keypressReportLocked(key, press)
}

// Caller holds hidLifecycle.RLock and keyboardLock, including when validating
// an auto-release generation before applying the transition.
func (u *UsbGadget) keypressReportLocked(key byte, press bool) (KeysDownState, error) {
	state := u.GetKeysDownState()
	modifier := state.Modifier
	keys := state.Keys

	if mask, isModifier := KeyCodeToMaskMap[key]; isModifier {
		// Set or clear only this side's bit; the other side is untouched.
		if press {
			modifier |= mask
		} else {
			modifier &^= mask
		}
	} else {
		found, empty := -1, -1
		for i, held := range keys {
			if held == key {
				found = i
			}
			if held == 0 && empty == -1 {
				empty = i
			}
		}
		if press {
			if found == -1 {
				if empty != -1 {
					keys[empty] = key
				} else {
					u.log.Error().Msg("keyboard buffer overflow, key not added")
					for i := range keys {
						keys[i] = hidErrorRollOver
					}
				}
			}
		} else if found != -1 {
			// Remove every occurrence, including duplicates in a full report.
			next := [hidKeyBufferSize]byte{}
			n := 0
			for _, held := range keys {
				if held != 0 && held != key {
					next[n] = held
					n++
				}
			}
			keys = next
		}
	}

	err := u.keyboardWriteHidFileLocked(modifier, keys[:])
	newState := u.UpdateKeysDown(modifier, keys[:])

	return newState, err
}

func (u *UsbGadget) KeypressReport(key byte, press bool) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	state, err := u.keypressReportLocked(key, press)
	if err != nil && !IsHIDBenignError(err) {
		u.log.Warn().Uint8("key", key).Bool("press", press).Msg("failed to report key")
	}

	isRolledOver := state.Keys[0] == hidErrorRollOver
	_, isModifier := KeyCodeToMaskMap[key]

	// Modifiers are tracked in the modifier byte and are released only by an
	// explicit key-up or a state clear. Arming a release timer for them is what
	// silently dropped Shift while it was still physically held.
	if !isModifier {
		switch {
		case isRolledOver, !press:
			u.cancelAutoRelease(key)
		default:
			u.scheduleAutoRelease(key)
		}
	}

	return err
}

// KeypressKeepAlive re-arms the auto-release deadline for every held key.
// Modifiers have no deadline, so they are skipped here.
func (u *UsbGadget) KeypressKeepAlive() error {
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()

	state := u.GetKeysDownState()

	for _, key := range state.Keys {
		if key == 0 {
			continue
		}
		if _, isModifier := KeyCodeToMaskMap[key]; isModifier {
			continue
		}
		u.scheduleAutoRelease(key)
	}

	return nil
}

func (u *UsbGadget) KeyboardReport(modifier uint8, keys []uint8) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	normalized := make([]byte, hidKeyBufferSize)
	copy(normalized, keys)

	u.keyboardLock.Lock()
	u.CancelAllAutoReleaseTimers()
	err := u.keyboardWriteHidFileLocked(modifier, normalized)
	if err != nil && !IsHIDBenignError(err) {
		u.log.Warn().Uint8("modifier", modifier).Uints8("keys", normalized).Msg("could not write keyboard report to hidg0")
	}
	u.UpdateKeysDown(modifier, normalized)
	u.keyboardLock.Unlock()

	return err
}
