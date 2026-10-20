package kvm

import (
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
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	
	resp, err := cliHTTPClient.Post(apiBaseURL+path, "application/json", strings.NewReader(string(jsonBody)))
	if err != nil {
		return cliConnectionError(err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		var errResp map[string]interface{}
		if json.Unmarshal(bodyBytes, &errResp) == nil {
			if msg, ok := errResp["error"].(string); ok {
				return fmt.Errorf("API error: %s", msg)
			}
		}
		return fmt.Errorf("API error: HTTP %d - %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
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
  cli    HID control via unix socket (mouse, keyboard, capture, status, signer)

Examples:
  kvm_app cli mouse move 100 200
  kvm_app cli mouse click left
  kvm_app cli keyboard type "Hello World"
  kvm_app cli capture
  kvm_app cli status
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
	Short: "Move mouse to position",
	Args:  cobra.ExactArgs(2),
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
			return apiPost("/mouse/relative", map[string]interface{}{
				"dx": x, "dy": y,
			})
		}
		return apiPost("/mouse/absolute", map[string]interface{}{
			"x": x, "y": y,
		})
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
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return apiPost("/keyboard/type", map[string]interface{}{
			"text": args[0],
		})
	},
}

// === Capture Command ===

var captureScreenshotPath = "/userdata/picokvm/screenshot/kvm_screenshot.jpg"

var captureCmd = &cobra.Command{
	Use:   "capture",
	Short: "Capture a JPEG screenshot",
	Long:  "Capture screenshot using hardware JPEG encoder and save to fixed path",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGetBytes("/capture")
		if err != nil {
			return err
		}

		if err := os.WriteFile(captureScreenshotPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write: %w", err)
		}
		fmt.Printf("Screenshot saved to %s (%d bytes)\n", captureScreenshotPath, len(data))
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
	"Enter":       0x28,
	"Escape":      0x29,
	"Backspace":   0x2A,
	"Tab":         0x2B,
	"Space":       0x2C,
	"Minus":       0x2D,
	"Equal":       0x2E,
	"LeftBrace":   0x2F,
	"RightBrace":  0x30,
	"Backslash":   0x31,
	"Semicolon":   0x33,
	"Quote":       0x34,
	"Grave":       0x35,
	"Comma":       0x36,
	"Dot":         0x37,
	"Slash":       0x38,
	"CapsLock":    0x39,
	"F1": 0x3A, "F2": 0x3B, "F3": 0x3C, "F4": 0x3D,
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
	cliRootCmd.AddCommand(keyboardCmd)

	cliRootCmd.AddCommand(captureCmd)

	signerKeygenCmd.Flags().String("output-dir", ".", "Directory for key output")
	signerCmd.AddCommand(signerKeygenCmd)
	signerSignCmd.Flags().String("key", "", "Path to private key")
	signerCmd.AddCommand(signerSignCmd)
	signerVerifyCmd.Flags().String("pubkey", "", "Path to public key file or hex-encoded public key")
	signerCmd.AddCommand(signerVerifyCmd)
	cliRootCmd.AddCommand(signerCmd)

	cliRootCmd.AddCommand(statusCmd)
}
