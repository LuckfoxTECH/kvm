package kvm

import (
	"fmt"
	"time"

	"kvm/internal/mcp"
)

// mcp_bridge.go wires the MCP package to the daemon. The package reaches the
// device only through the three interfaces below; everything here is a thin
// adapter onto the same primitives in input_actions.go that the local CLI API
// calls, so a fix on one surface can never miss the other.

// mcpInput adapts the HID helpers.
type mcpInput struct{}

func (mcpInput) MoveToPixel(x, y int) (int, int, error) { return moveMouseToPixel(x, y) }
func (mcpInput) MoveRelative(dx, dy int) (int, int, error) {
	return moveMouseRelative(dx, dy)
}
func (mcpInput) Click(button string, clicks int) error { return clickMouse(button, clicks) }
func (mcpInput) Scroll(steps int8, wheelX int8, mouseMode string) error {
	return wheelScroll(steps, wheelX, mouseMode)
}

func (mcpInput) KeyCode(name string) (uint8, bool) {
	return lookupKeyCode(name)
}

// ModifierBit lets keyboard_key press and hold a modifier on its own, not only
// as part of a combo.
func (mcpInput) ModifierBit(name string) (uint8, bool) {
	return keyModifier(name)
}

func (mcpInput) KeyboardReport(modifier uint8, keys []uint8) error {
	return keyboardReport(modifier, keys)
}

func (mcpInput) TapKeys(modifier uint8, keys []uint8) error { return tapKeys(modifier, keys) }
func (mcpInput) ReleaseKeys() error                         { return releaseKeys() }
func (mcpInput) ParseCombo(names []string) (uint8, []uint8, error) {
	return parseCombo(names)
}

func (mcpInput) TypeText(text string, delay time.Duration) error {
	return typeASCIIText(text, delay)
}

func (mcpInput) AbsPosition() (int, int) { return gadget.GetAbsMousePosition() }
func (mcpInput) MaxTextLength() int      { return maxTypeTextLength }

// KeysDownState exposes what the HID layer actually holds, so a test can assert
// on the real modifier byte instead of inferring it from the reports it sent.
func (mcpInput) KeysDownState() (uint8, []uint8) {
	state := gadget.GetKeysDownState()
	keys := make([]uint8, 0, len(state.Keys))
	for _, code := range state.Keys {
		// the report is a fixed six-slot array; the padding zeros are not keys
		if code != 0 {
			keys = append(keys, code)
		}
	}
	return state.Modifier, keys
}

// KeyboardLedState exposes the LED byte the host last sent, so a caller can tell
// whether the lock lights it sees are the host's or just the default zero.
func (mcpInput) KeyboardLedState() mcp.KeyboardLeds {
	state := gadget.GetKeyboardState()
	return mcp.KeyboardLeds{
		Raw:        state.Byte(),
		Reported:   state.Reported,
		NumLock:    state.NumLock,
		CapsLock:   state.CapsLock,
		ScrollLock: state.ScrollLock,
		Compose:    state.Compose,
		Kana:       state.Kana,
		Shift:      state.Shift,
	}
}

// mcpScreen adapts the capture path.
type mcpScreen struct{}

func (mcpScreen) Capture(timeout time.Duration) ([]byte, error) {
	// run the blocking capture in the background so a stuck encoder cannot hold
	// the tool call open
	type shot struct {
		data []byte
		err  error
	}
	done := make(chan shot, 1)
	go func() {
		data, err := captureScreenshot("jpeg")
		done <- shot{data: data, err: err}
	}()

	select {
	case result := <-done:
		return result.data, result.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("screenshot timed out after %dms", timeout.Milliseconds())
	}
}

func (mcpScreen) Save(data []byte) (string, error) { return persistSnapshot(data) }

func (mcpScreen) State() (mcp.VideoState, error) {
	state, err := rpcGetVideoState()
	if err != nil {
		return mcp.VideoState{}, err
	}
	return mcp.VideoState{
		Width:  state.Width,
		Height: state.Height,
		FPS:    state.FramePerSecond,
		Ready:  state.Ready,
		Error:  state.Error,
	}, nil
}

// mcpKeyStore adapts the saved configuration.
type mcpKeyStore struct{}

func (mcpKeyStore) APIKey() string { return config.APIKey }
func (mcpKeyStore) Enabled() bool  { return !config.MCPDisabled }

func (mcpKeyStore) SetEnabled(enabled bool) error {
	config.MCPDisabled = !enabled
	return SaveConfig()
}

func (mcpKeyStore) RegenerateKey() (string, error) {
	key, err := generateAPIKey()
	if err != nil {
		return "", err
	}
	config.APIKey = key
	if err := SaveConfig(); err != nil {
		return "", err
	}
	return key, nil
}

// mcpAuditPath receives one JSON line per MCP tool call. It lives next to the
// config so it lands on the same writable partition and survives reboots.
const mcpAuditPath = "/userdata/picokvm/mcp_audit.jsonl"

// initMCP installs the device side of the MCP package. It must run before the
// management API is reachable, so the last call happens early in main.
func initMCP() {
	mcp.Configure(mcp.Deps{
		Input:     mcpInput{},
		Screen:    mcpScreen{},
		Key:       mcpKeyStore{},
		Log:       logger,
		AuditPath: mcpAuditPath,
	})

	// seed the live state early: the server itself starts in a goroutine, but
	// the management API can be queried before that
	mcp.ApplyAPIKey(config.APIKey)
	mcp.PublishEnabled(!config.MCPDisabled)
}
