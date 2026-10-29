package kvm

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var cliRootCmd = &cobra.Command{
	Use:           "kvm_app cli",
	Short:         "PicoKVM CLI tools",
	Long:          `Command line interface for controlling HID devices via API`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func RunCLI(args []string) {
	cliRootCmd.SetArgs(args)
	if err := cliRootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// apiBaseURL is only a placeholder host: cliHTTPClient always dials the unix
// socket, so the host part is never resolved and no TCP port is opened.
var apiBaseURL = "http://unix/api/lan"

var cliHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", cliAPISocketPath)
		},
	},
}

// cliConnectionError keeps the "is kvm_app running?" hint but also names the
// socket, so a permission or missing-daemon problem is obvious.
func cliConnectionError(err error) error {
	return fmt.Errorf("failed to connect to kvm_app at %s: %w\nIs kvm_app running?", cliAPISocketPath, err)
}

func apiPost(path string, body interface{}) error {
	_, err := apiPostJSON(path, body)
	return err
}

// apiPostJSON posts a command and decodes the JSON answer, for the endpoints
// that return state instead of a bare acknowledgement.
func apiPostJSON(path string, body interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	resp, err := cliHTTPClient.Post(apiBaseURL+path, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, cliConnectionError(err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		var errResp map[string]interface{}
		if json.Unmarshal(bodyBytes, &errResp) == nil {
			if msg, ok := errResp["error"].(string); ok {
				return nil, fmt.Errorf("API error: %s", msg)
			}
		}
		return nil, fmt.Errorf("API error: HTTP %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if len(bodyBytes) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func apiGetBytes(path string) ([]byte, error) {
	resp, err := cliHTTPClient.Get(apiBaseURL + path)
	if err != nil {
		return nil, cliConnectionError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: HTTP %d - %s", resp.StatusCode, string(bodyBytes))
	}
	return io.ReadAll(resp.Body)
}

func apiGetJSON(path string) (map[string]interface{}, error) {
	resp, err := cliHTTPClient.Get(apiBaseURL + path)
	if err != nil {
		return nil, cliConnectionError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: HTTP %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func ShowHelp() {
	fmt.Println(`PicoKVM - Remote KVM over IP

Usage:
  kvm_app                    Start the main web service (default)
  kvm_app cli <command>     Run CLI commands for HID control

Commands:
  cli    HID control via unix socket (mouse, keyboard, capture, status, signer, mcp)

Examples:
  kvm_app cli mouse move 100 200
  kvm_app cli mouse click left
  kvm_app cli keyboard type "Hello World"
  kvm_app cli capture
  kvm_app cli status
  kvm_app cli mcp status
  kvm_app cli mcp regenerate
  kvm_app cli signer keygen
  kvm_app cli signer sign --key ota_ed25519.key firmware.bin
  kvm_app cli signer verify --pubkey ota_ed25519.pub firmware.bin

For more help on a specific command, use:
  kvm_app cli --help`)
}

// === Mouse Commands ===

var mouseCmd = &cobra.Command{
	Use:   "mouse",
	Short: "Mouse control commands",
}

var mouseMoveCmd = &cobra.Command{
	Use:   "move [x] [y]",
	Short: "Move mouse to a pixel position",
	Long: `Move the mouse.

Without --relative, x and y are pixels of the current video input, the same unit
the MCP mouse_move tool takes; values outside the screen are clamped and the
coordinate is scaled to HID logical units internally.

With --relative, x and y are HID steps instead: one signed 8-bit report, clamped
to -127..127. A pixel offset is meaningless there because the target applies its
own pointer acceleration.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		x, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid x coordinate: %w", err)
		}
		y, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid y coordinate: %w", err)
		}

		relative, _ := cmd.Flags().GetBool("relative")
		if relative {
			resp, err := apiPostJSON("/mouse/relative", map[string]interface{}{
				"dx": x, "dy": y,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Mouse moved by (%v, %v)\n", resp["dx"], resp["dy"])
			return nil
		}

		resp, err := apiPostJSON("/mouse/absolute", map[string]interface{}{
			"x": x, "y": y,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Mouse moved to pixel (%v, %v)\n", resp["x"], resp["y"])
		return nil
	},
}

var mouseClickCmd = &cobra.Command{
	Use:   "click [button]",
	Short: "Click a mouse button",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return apiPost("/mouse/click", map[string]interface{}{
			"button": args[0],
		})
	},
}

var mouseScrollCmd = &cobra.Command{
	Use:   "scroll [delta]",
	Short: "Scroll mouse wheel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		delta, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid delta: %w", err)
		}
		return apiPost("/mouse/scroll", map[string]interface{}{
			"delta": delta,
		})
	},
}

var mouseDownCmd = &cobra.Command{
	Use:   "down [button]",
	Short: "Press and hold a mouse button",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// API doesn't support hold yet, simulate with click
		return apiPost("/mouse/click", map[string]interface{}{
			"button": args[0],
		})
	},
}

var mouseUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Release all mouse buttons",
	RunE: func(cmd *cobra.Command, args []string) error {
		// API doesn't support hold yet, no-op
		return nil
	},
}

// === Keyboard Commands ===

var keyboardCmd = &cobra.Command{
	Use:   "keyboard",
	Short: "Keyboard control commands",
}

var keyboardKeyCmd = &cobra.Command{
	Use:   "key [keyname]",
	Short: "Press a key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return apiPost("/keyboard/key", map[string]interface{}{
			"key": args[0],
		})
	},
}

var keyboardComboCmd = &cobra.Command{
	Use:   "combo [keys...]",
	Short: "Press a key combination",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return apiPost("/keyboard/combo", map[string]interface{}{
			"keys": args,
		})
	},
}

var keyboardTypeCmd = &cobra.Command{
	Use:   "type [text]",
	Short: "Type a text string",
	Long: `Type an ASCII text string into the remote host.

A short delay is inserted between two keystrokes because the remote USB HID
stack drops keys when reports are sent back to back. --delay overrides it,
0 types as fast as the stack accepts.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		payload := map[string]interface{}{"text": args[0]}
		if cmd.Flags().Changed("delay") {
			delay, _ := cmd.Flags().GetInt("delay")
			payload["delayMs"] = delay
		}
		resp, err := apiPostJSON("/keyboard/type", payload)
		if err != nil {
			return err
		}
		// the server knows the final rhythm (it clamps the delay and applies the
		// default), so report what actually happened rather than what was asked
		if message, ok := resp["message"].(string); ok {
			fmt.Println(message)
		}
		return nil
	},
}

// === Capture Command ===

var captureOutputPath string

var captureCmd = &cobra.Command{
	Use:   "capture",
	Short: "Capture a JPEG screenshot",
	Long: "Capture screenshot using hardware JPEG encoder and save it to a timestamped file.\n" +
		"Use --output to pick the path, or --output - to write the JPEG to stdout.",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGetBytes("/capture")
		if err != nil {
			return err
		}

		if captureOutputPath == "-" {
			if _, err := os.Stdout.Write(data); err != nil {
				return fmt.Errorf("failed to write to stdout: %w", err)
			}
			return nil
		}

		path := captureOutputPath
		if path == "" {
			path, err = persistSnapshot(data)
			if err != nil {
				return err
			}
		} else if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}

		fmt.Printf("Screenshot saved to %s (%d bytes)\n", path, len(data))
		return nil
	},
}

// === Status Command ===

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show device status",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGetJSON("/video/state")
		if err != nil {
			return err
		}

		fmt.Printf("Video State:\n")
		fmt.Printf("  Ready:  %v\n", resp["ready"])
		fmt.Printf("  Error:  %s\n", resp["error"])
		fmt.Printf("  Size:   %dx%d\n", int(resp["width"].(float64)), int(resp["height"].(float64)))
		fmt.Printf("  FPS:    %.1f\n", resp["fps"])
		return nil
	},
}

// === Key Maps ===

var keyNameToCode = map[string]uint8{
	"a": 0x04, "b": 0x05, "c": 0x06, "d": 0x07,
	"e": 0x08, "f": 0x09, "g": 0x0A, "h": 0x0B,
	"i": 0x0C, "j": 0x0D, "k": 0x0E, "l": 0x0F,
	"m": 0x10, "n": 0x11, "o": 0x12, "p": 0x13,
	"q": 0x14, "r": 0x15, "s": 0x16, "t": 0x17,
	"u": 0x18, "v": 0x19, "w": 0x1A, "x": 0x1B,
	"y": 0x1C, "z": 0x1D,
	"1": 0x1E, "2": 0x1F, "3": 0x20, "4": 0x21,
	"5": 0x22, "6": 0x23, "7": 0x24, "8": 0x25,
	"9": 0x26, "0": 0x27,
	"Enter":      0x28,
	"Escape":     0x29,
	"Backspace":  0x2A,
	"Tab":        0x2B,
	"Space":      0x2C,
	"Minus":      0x2D,
	"Equal":      0x2E,
	"LeftBrace":  0x2F,
	"RightBrace": 0x30,
	"Backslash":  0x31,
	"Semicolon":  0x33,
	"Quote":      0x34,
	"Grave":      0x35,
	"Comma":      0x36,
	"Dot":        0x37,
	"Slash":      0x38,
	"CapsLock":   0x39,
	"F1":         0x3A, "F2": 0x3B, "F3": 0x3C, "F4": 0x3D,
	"F5": 0x3E, "F6": 0x3F, "F7": 0x40, "F8": 0x41,
	"F9": 0x42, "F10": 0x43, "F11": 0x44, "F12": 0x45,
	"PrintScreen": 0x46,
	"ScrollLock":  0x47,
	"Pause":       0x48,
	"Insert":      0x49,
	"Home":        0x4A,
	"PageUp":      0x4B,
	"Delete":      0x4C,
	"End":         0x4D,
	"PageDown":    0x4E,
	"Right":       0x4F,
	"Left":        0x50,
	"Down":        0x51,
	"Up":          0x52,
}

// shiftedSymbols maps every printable ASCII character that needs Shift on a US
// layout to the usage ID of the key it sits on. Without this table charToKeyCode
// silently dropped all of them, so `echo a > f && cat f` arrived as
// `echo a f cat f`: the redirect and the && never reached the host.
var shiftedSymbols = map[uint8]uint8{
	'!': 0x1E, '@': 0x1F, '#': 0x20, '$': 0x21, '%': 0x22,
	'^': 0x23, '&': 0x24, '*': 0x25, '(': 0x26, ')': 0x27,
	'_': 0x2D, '+': 0x2E, '{': 0x2F, '}': 0x30, '|': 0x31,
	':': 0x33, '"': 0x34, '~': 0x35, '<': 0x36, '>': 0x37, '?': 0x38,
}

func charToKeyCode(char uint8) (keyCode uint8, modifier uint8, ok bool) {
	if char >= 'a' && char <= 'z' {
		return keyNameToCode[string(char)], 0, true
	}
	if char >= 'A' && char <= 'Z' {
		return keyNameToCode[strings.ToLower(string(char))], 0x02, true
	}
	if char >= '0' && char <= '9' {
		return keyNameToCode[string(char)], 0, true
	}
	if char == ' ' {
		return 0x2C, 0, true
	}
	switch char {
	case '-':
		return 0x2D, 0, true
	case '=':
		return 0x2E, 0, true
	case '[':
		return 0x2F, 0, true
	case ']':
		return 0x30, 0, true
	case '\\':
		return 0x31, 0, true
	case ';':
		return 0x33, 0, true
	case '\'':
		return 0x34, 0, true
	case '`':
		return 0x35, 0, true
	case ',':
		return 0x36, 0, true
	case '.':
		return 0x37, 0, true
	case '/':
		return 0x38, 0, true
	}

	// Shifted punctuation. The table is keyed by the character itself, so the
	// caller does not have to know which key it sits on.
	if code, found := shiftedSymbols[char]; found {
		return code, 0x02, true
	}

	return 0, 0, false
}

var signerCmd = &cobra.Command{
	Use:   "signer",
	Short: "OTA firmware signing tool",
	Long:  "Generate keys, sign, and verify OTA firmware using Ed25519",
}

var signerKeygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate Ed25519 key pair",
	RunE: func(cmd *cobra.Command, args []string) error {
		outputDir, _ := cmd.Flags().GetString("output-dir")

		pubKeyPath := filepath.Join(outputDir, "ota_ed25519.pub")
		privKeyPath := filepath.Join(outputDir, "ota_ed25519.key")

		if _, err := os.Stat(privKeyPath); err == nil {
			return fmt.Errorf("private key already exists at %s, refusing to overwrite", privKeyPath)
		}

		publicKey, privateKey, err := ed25519.GenerateKey(nil)
		if err != nil {
			return fmt.Errorf("generating key pair: %w", err)
		}

		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		if err := os.WriteFile(privKeyPath, privateKey, 0600); err != nil {
			return fmt.Errorf("writing private key: %w", err)
		}

		if err := os.WriteFile(pubKeyPath, publicKey, 0644); err != nil {
			return fmt.Errorf("writing public key: %w", err)
		}

		fmt.Println(hex.EncodeToString(publicKey))
		fmt.Fprintf(os.Stderr, "Private key: %s\n", privKeyPath)
		fmt.Fprintf(os.Stderr, "Public key:  %s\n", pubKeyPath)
		return nil
	},
}

var signerSignCmd = &cobra.Command{
	Use:   "sign --key <private-key> <firmware-file>",
	Short: "Sign a firmware file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyArg, _ := cmd.Flags().GetString("key")
		filePath := args[0]

		if keyArg == "" {
			return fmt.Errorf("--key is required")
		}

		privateKey, err := os.ReadFile(keyArg)
		if err != nil {
			privateKey, err = hex.DecodeString(keyArg)
			if err != nil {
				return fmt.Errorf("invalid private key: not a valid file path or hex string")
			}
		}

		if len(privateKey) != ed25519.PrivateKeySize {
			return fmt.Errorf("invalid private key size: got %d bytes, expected %d", len(privateKey), ed25519.PrivateKeySize)
		}

		fileData, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading firmware file: %w", err)
		}

		fileHash := sha256.Sum256(fileData)
		signature := ed25519.Sign(ed25519.PrivateKey(privateKey), fileHash[:])

		sigPath := filePath + ".sig"
		if err := os.WriteFile(sigPath, signature, 0644); err != nil {
			return fmt.Errorf("writing signature: %w", err)
		}

		hash := sha256.Sum256(fileData)
		fmt.Println(hex.EncodeToString(hash[:]))
		fmt.Fprintf(os.Stderr, "Signature written to: %s\n", sigPath)
		return nil
	},
}

var signerVerifyCmd = &cobra.Command{
	Use:   "verify [--pubkey <pubkey-path-or-hex>] <firmware-file> [<sig-file>]",
	Short: "Verify firmware signature",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		pubKeyArg, _ := cmd.Flags().GetString("pubkey")
		filePath := args[0]
		sigPath := ""
		if len(args) > 1 {
			sigPath = args[1]
		}

		if sigPath == "" {
			sigPath = filePath + ".sig"
		}

		var publicKey ed25519.PublicKey

		if pubKeyArg == "" {
			keyStr := strings.TrimSpace(builtOtaPublicKey)
			if keyStr == "" {
				return fmt.Errorf("no --pubkey provided and no OTA public key embedded in binary")
			}
			keyBytes, err := hex.DecodeString(keyStr)
			if err != nil {
				return fmt.Errorf("decoding embedded public key hex: %w", err)
			}
			publicKey = ed25519.PublicKey(keyBytes)
		} else if _, err := os.Stat(pubKeyArg); err == nil {
			keyBytes, err := os.ReadFile(pubKeyArg)
			if err != nil {
				return fmt.Errorf("reading public key file: %w", err)
			}
			publicKey = ed25519.PublicKey(keyBytes)
		} else {
			keyBytes, err := hex.DecodeString(pubKeyArg)
			if err != nil {
				return fmt.Errorf("decoding public key hex: %w", err)
			}
			publicKey = ed25519.PublicKey(keyBytes)
		}

		if len(publicKey) != ed25519.PublicKeySize {
			return fmt.Errorf("invalid public key size: got %d bytes, expected %d", len(publicKey), ed25519.PublicKeySize)
		}

		fileData, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading firmware file: %w", err)
		}

		sigBytes, err := os.ReadFile(sigPath)
		if err != nil {
			return fmt.Errorf("reading signature file: %w", err)
		}

		if len(sigBytes) != ed25519.SignatureSize {
			return fmt.Errorf("invalid signature size: got %d bytes, expected %d", len(sigBytes), ed25519.SignatureSize)
		}

		fileHash := sha256.Sum256(fileData)
		if !ed25519.Verify(publicKey, fileHash[:], sigBytes) {
			return fmt.Errorf("VERIFICATION FAILED: signature is invalid")
		}

		fmt.Fprintf(os.Stderr, "VERIFICATION OK: signature is valid\n")
		fmt.Fprintf(os.Stderr, "SHA256: %s\n", hex.EncodeToString(fileHash[:]))
		return nil
	},
}

// === MCP Commands ===

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Inspect and manage the MCP server",
	Long: `Inspect and manage the MCP server over the local socket.

These commands exist so the MCP endpoint can be inspected, disabled or have its
key rotated when the Web UI is unreachable. The same operations are available in
the Web UI; the socket permissions (0600, root) are the access control here.`,
}

var mcpStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show MCP server state",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGetJSON("/mcp/config")
		if err != nil {
			return err
		}

		enabled, _ := resp["enabled"].(bool)
		listening, _ := resp["listening"].(bool)
		keySet, _ := resp["apiKeySet"].(bool)
		port := 0
		if v, ok := resp["port"].(float64); ok {
			port = int(v)
		}

		fmt.Printf("MCP Server:\n")
		fmt.Printf("  Enabled:   %v\n", enabled)
		fmt.Printf("  Listening: %v (port %d)\n", listening, port)
		if keySet {
			masked, _ := resp["apiKey"].(string)
			fmt.Printf("  API Key: %s (masked)\n", masked)
		} else {
			fmt.Printf("  API Key: not set, every request is refused\n")
		}
		return nil
	},
}

var mcpEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Start accepting MCP connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		return setMCPEnabled(true)
	},
}

var mcpDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Refuse MCP connections and close the listener",
	RunE: func(cmd *cobra.Command, args []string) error {
		return setMCPEnabled(false)
	},
}

var mcpRegenerateCmd = &cobra.Command{
	Use:   "regenerate",
	Short: "Rotate the MCP API key and print the new one",
	Long: `Rotate the MCP API key.

The new key is printed once. Every connected MCP client has to be updated, the
old key stops working immediately.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiPostJSON("/mcp/key/regenerate", map[string]interface{}{})
		if err != nil {
			return err
		}

		key, _ := resp["apiKey"].(string)
		fmt.Printf("New MCP API key: %s\n", key)
		fmt.Printf("Clients using the old key are refused from now on.\n")
		return nil
	},
}

func setMCPEnabled(enabled bool) error {
	resp, err := apiPostJSON("/mcp/config", map[string]interface{}{"enabled": enabled})
	if err != nil {
		return err
	}

	state, _ := resp["enabled"].(bool)
	if state == enabled {
		fmt.Printf("MCP server %s\n", map[bool]string{true: "enabled", false: "disabled"}[enabled])
		return nil
	}
	return fmt.Errorf("API reported enabled=%v after asking for %v", state, enabled)
}

func init() {
	mouseMoveCmd.Flags().Bool("relative", false, "Use relative positioning")
	mouseCmd.AddCommand(mouseMoveCmd)
	mouseCmd.AddCommand(mouseClickCmd)
	mouseCmd.AddCommand(mouseScrollCmd)
	mouseCmd.AddCommand(mouseDownCmd)
	mouseCmd.AddCommand(mouseUpCmd)
	cliRootCmd.AddCommand(mouseCmd)

	keyboardCmd.AddCommand(keyboardKeyCmd)
	keyboardCmd.AddCommand(keyboardComboCmd)
	keyboardCmd.AddCommand(keyboardTypeCmd)
	keyboardTypeCmd.Flags().Int("delay", 30, "Delay between two keystrokes in ms, 0..1000")
	cliRootCmd.AddCommand(keyboardCmd)

	cliRootCmd.AddCommand(captureCmd)
	captureCmd.Flags().StringVarP(&captureOutputPath, "output", "o", "", "Output path for the JPEG (default: timestamped file in the screenshot directory, - for stdout)")

	signerKeygenCmd.Flags().String("output-dir", ".", "Directory for key output")
	signerCmd.AddCommand(signerKeygenCmd)
	signerSignCmd.Flags().String("key", "", "Path to private key")
	signerCmd.AddCommand(signerSignCmd)
	signerVerifyCmd.Flags().String("pubkey", "", "Path to public key file or hex-encoded public key")
	signerCmd.AddCommand(signerVerifyCmd)
	cliRootCmd.AddCommand(signerCmd)

	cliRootCmd.AddCommand(statusCmd)

	mcpCmd.AddCommand(mcpStatusCmd)
	mcpCmd.AddCommand(mcpEnableCmd)
	mcpCmd.AddCommand(mcpDisableCmd)
	mcpCmd.AddCommand(mcpRegenerateCmd)
	cliRootCmd.AddCommand(mcpCmd)
}
