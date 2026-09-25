package kvm

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"kvm/internal/logging"
	"kvm/internal/network"
	"kvm/internal/usbgadget"
)

type WakeOnLanDevice struct {
	Name       string `json:"name"`
	MacAddress string `json:"macAddress"`
}

type TurnServer struct {
	URL        string `json:"url"`
	Username   string `json:"username"`
	Credential string `json:"credential"`
}

// Constants for keyboard macro limits
const (
	MaxMacrosPerDevice = 25
	MaxStepsPerMacro   = 10
	MaxKeysPerStep     = 10
	MinStepDelay       = 50
	MaxStepDelay       = 2000
)

type KeyboardMacroStep struct {
	Keys      []string `json:"keys"`
	Modifiers []string `json:"modifiers"`
	Delay     int      `json:"delay"`
}

func (s *KeyboardMacroStep) Validate() error {
	if len(s.Keys) > MaxKeysPerStep {
		return fmt.Errorf("too many keys in step (max %d)", MaxKeysPerStep)
	}

	if s.Delay < MinStepDelay {
		s.Delay = MinStepDelay
	} else if s.Delay > MaxStepDelay {
		s.Delay = MaxStepDelay
	}

	return nil
}

type KeyboardMacro struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Steps     []KeyboardMacroStep `json:"steps"`
	SortOrder int                 `json:"sortOrder,omitempty"`
}

func (m *KeyboardMacro) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("macro name cannot be empty")
	}

	if len(m.Steps) == 0 {
		return fmt.Errorf("macro must have at least one step")
	}

	if len(m.Steps) > MaxStepsPerMacro {
		return fmt.Errorf("too many steps in macro (max %d)", MaxStepsPerMacro)
	}

	for i := range m.Steps {
		if err := m.Steps[i].Validate(); err != nil {
			return fmt.Errorf("invalid step %d: %w", i+1, err)
		}
	}

	return nil
}

type Config struct {
	STUN                       string                 `json:"stun"`
	TurnServers                []TurnServer           `json:"turn_servers"`
	JigglerEnabled             bool                   `json:"jiggler_enabled"`
	UpdateDownloadProxy        string                 `json:"update_download_proxy"`
	HashedPassword             string                 `json:"hashed_password"`
	LocalAuthToken             string                 `json:"local_auth_token"`
	LocalAuthMode              string                 `json:"localAuthMode"` // TODO: fix it with migration
	LocalLoopbackOnly          bool                   `json:"local_loopback_only"`
	UsbEnhancedDetection       bool                   `json:"usb_enhanced_detection"`
	WakeOnLanDevices           []WakeOnLanDevice      `json:"wake_on_lan_devices"`
	KeyboardMacros             []KeyboardMacro        `json:"keyboard_macros"`
	KeyboardLayout             string                 `json:"keyboard_layout"`
	EdidString                 string                 `json:"hdmi_edid_string"`
	ForceHpd                   bool                   `json:"force_hpd"`
	ActiveExtension            string                 `json:"active_extension"`
	DisplayRotation            string                 `json:"display_rotation"`
	DisplayMaxBrightness       int                    `json:"display_max_brightness"`
	DisplayDimAfterSec         int                    `json:"display_dim_after_sec"`
	DisplayOffAfterSec         int                    `json:"display_off_after_sec"`
	TLSMode                    string                 `json:"tls_mode"` // options: "self-signed", "user-defined", ""
	UsbConfig                  *usbgadget.Config      `json:"usb_config"`
	UsbDevices                 *usbgadget.Devices     `json:"usb_devices"`
	NetworkConfig              *network.NetworkConfig `json:"network_config"`
	AppliedNetworkConfig       *network.NetworkConfig `json:"applied_network_config,omitempty"`
	DefaultLogLevel            string                 `json:"default_log_level"`
	TailScaleAutoStart         bool                   `json:"tailscale_autostart"`
	TailScaleXEdge             bool                   `json:"tailscale_xedge"`
	ZeroTierNetworkID          string                 `json:"zerotier_network_id"`
	ZeroTierAutoStart          bool                   `json:"zerotier_autostart"`
	FrpcAutoStart              bool                   `json:"frpc_autostart"`
	FrpcToml                   string                 `json:"frpc_toml"`
	CloudflaredAutoStart       bool                   `json:"cloudflared_autostart"`
	CloudflaredToken           string                 `json:"cloudflared_token"`
	IO0Status                  bool                   `json:"io0_status"`
	IO1Status                  bool                   `json:"io1_status"`
	AudioMode                  string                 `json:"audio_mode"`
	TimeZone                   string                 `json:"time_zone"`
	LEDGreenMode               string                 `json:"led_green_mode"`
	LEDYellowMode              string                 `json:"led_yellow_mode"`
	AutoMountImage             *AutoMountImageConfig  `json:"auto_mount_image,omitempty"`
	EasytierAutoStart          bool                   `json:"easytier_autostart"`
	EasytierConfig             EasytierConfig         `json:"easytier_config"`
	VntAutoStart               bool                   `json:"vnt_autostart"`
	VntConfig                  VntConfig              `json:"vnt_config"`
	WireguardAutoStart         bool                   `json:"wireguard_autostart"`
	WireguardConfig            WireguardConfig        `json:"wireguard_config"`
	NetbirdAutoStart           bool                   `json:"netbird_autostart"`
	NetbirdManagementURL       string                 `json:"netbird_management_url"`
	NpuAppEnabled              bool                   `json:"npu_app_enabled"`
	Firewall                   *FirewallConfig        `json:"firewall"`
	APIKey                     string                 `json:"api_key"`
	PersistedVirtualMediaState *VirtualMediaState     `json:"persisted_virtual_media_state,omitempty"`
	SerialBaudRate             int                    `json:"serial_baud_rate,omitempty"`
	SerialDataBits             int                    `json:"serial_data_bits,omitempty"`
	SerialStopBits             string                 `json:"serial_stop_bits,omitempty"` // "1", "1.5", "2"
	SerialParity               string                 `json:"serial_parity,omitempty"`   // "none", "odd", "even", "mark", "space"
	KvmSwitch                  *KvmSwitchConfig       `json:"kvm_switch,omitempty"`
}

type KvmSwitchConfig struct {
	Enabled         bool           `json:"enabled"`
	SwitchIP        string         `json:"switch_ip"`
	SwitchPort      int            `json:"switch_port"`
	TotalPorts      int            `json:"total_ports"`
	PortNames       map[int]string `json:"port_names"`
	HotkeysEnabled  bool           `json:"hotkeys_enabled"`
	PollIntervalSec int            `json:"poll_interval_sec"`
}

type AutoMountImageConfig struct {
	Filename string `json:"filename"` // e.g. "ubuntu-24.04.iso"
	Source   string `json:"source"`   // "kvm" or "sd"
}

type FirewallConfig struct {
	Base         FirewallBaseRule   `json:"base"`
	Rules        []FirewallRule     `json:"rules"`
	PortForwards []FirewallPortRule `json:"portForwards"`
}

type FirewallBaseRule struct {
	InputPolicy   string `json:"inputPolicy"`
	OutputPolicy  string `json:"outputPolicy"`
	ForwardPolicy string `json:"forwardPolicy"`
}

type FirewallRule struct {
	Chain           string   `json:"chain"`
	SourceIP        string   `json:"sourceIP"`
	SourcePort      *int     `json:"sourcePort,omitempty"`
	Protocols       []string `json:"protocols"`
	DestinationIP   string   `json:"destinationIP"`
	DestinationPort *int     `json:"destinationPort,omitempty"`
	Action          string   `json:"action"`
	Comment         string   `json:"comment"`
}

type FirewallPortRule struct {
	Chain           string   `json:"chain,omitempty"`
	Managed         *bool    `json:"managed,omitempty"`
	SourcePort      int      `json:"sourcePort"`
	Protocols       []string `json:"protocols"`
	DestinationIP   string   `json:"destinationIP"`
	DestinationPort int      `json:"destinationPort"`
	Comment         string   `json:"comment"`
}

type VntConfig struct {
	Token      string `json:"token"`
	DeviceId   string `json:"device_id"`
	Name       string `json:"name"`
	ServerAddr string `json:"server_addr"`
	ConfigMode string `json:"config_mode"` // "params" or "file"
	ConfigFile string `json:"config_file"`
	Model      string `json:"model"`
	Password   string `json:"password"`
}

type WireguardConfig struct {
	NetworkName string `json:"network_name"`
	ConfigFile  string `json:"config_file"`
}

const (
	configPath   = "/userdata/kvm_config.json"
	sdConfigPath = "/mnt/sdcard/kvm_config.json"
)

// builtOtaPublicKey is the hex-encoded Ed25519 public key for OTA signature verification,
// injected via -ldflags at build time. Empty string disables signature verification.
var builtOtaPublicKey = ""

var defaultConfig = &Config{
	STUN:                 "stun:stun.l.google.com:19302",
	TurnServers:          []TurnServer{},
	ActiveExtension:      "",
	KeyboardMacros:       []KeyboardMacro{},
	DisplayRotation:      "180",
	TimeZone:             "UTC-8",
	KeyboardLayout:       "en_US",
	DisplayMaxBrightness: 64,
	DisplayDimAfterSec:   120,  // 2 minutes
	DisplayOffAfterSec:   1800, // 30 minutes
	TLSMode:              "",
	ForceHpd:             false,
	UsbEnhancedDetection: true,
	UsbConfig: &usbgadget.Config{
		VendorId:     "0x1d6b", // The Linux Foundation
		ProductId:    "0x0104", // Multifunction Composite Gadget
		SerialNumber: "",
		Manufacturer: "KVM",
		Product:      "USB Emulation Device",
	},
	UsbDevices: &usbgadget.Devices{
		AbsoluteMouse: true,
		RelativeMouse: true,
		Keyboard:      true,
		MassStorage:   true,
		Audio:         false, // At any given time, only one of Audio and Mtp can be set to true
		Mtp:           false,
	},
	NetworkConfig:        &network.NetworkConfig{},
	AppliedNetworkConfig: nil,
	DefaultLogLevel:      "INFO",
	ZeroTierAutoStart:    false,
	TailScaleAutoStart:   false,
	TailScaleXEdge:       false,
	FrpcAutoStart:        false,
	CloudflaredAutoStart: false,
	IO0Status:            false,
	IO1Status:            false,
	AudioMode:            "disabled",
	LEDGreenMode:         "network-rx",
	LEDYellowMode:        "kernel-activity",
	AutoMountImage:       &AutoMountImageConfig{Filename: "system_info.img", Source: "kvm"},
	WireguardAutoStart:   false,
	NpuAppEnabled:        false,
	Firewall: &FirewallConfig{
		Base: FirewallBaseRule{
			InputPolicy:   "accept",
			OutputPolicy:  "accept",
			ForwardPolicy: "accept",
		},
		Rules:        []FirewallRule{},
		PortForwards: []FirewallPortRule{},
	},
	KvmSwitch: &KvmSwitchConfig{
		Enabled:         false,
		SwitchIP:        "",
		SwitchPort:      5000,
		TotalPorts:      4,
		PortNames:       map[int]string{1: "Server 1", 2: "Server 2", 3: "Server 3", 4: "Server 4"},
		HotkeysEnabled:  true,
		PollIntervalSec: 5,
	},
}

var (
	config     *Config
	configLock = &sync.Mutex{}
)

type ConfigMigrationFunc func(raw json.RawMessage) (json.RawMessage, error)

var configMigrations = []ConfigMigrationFunc{
	migrateLocalAuthMode,
}

func migrateLocalAuthMode(raw json.RawMessage) (json.RawMessage, error) {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, fmt.Errorf("migrateLocalAuthMode: failed to parse config JSON: %w", err)
	}

	if authModeRaw, exists := rawMap["localAuthMode"]; exists {
		var authMode string
		if err := json.Unmarshal(authModeRaw, &authMode); err == nil {
			if authMode != "" {
				validModes := map[string]bool{"password": true, "noPassword": true}
				if !validModes[authMode] {
					rawMap["localAuthMode"] = json.RawMessage(`"password"`)
				}
			}
		} else {
			delete(rawMap, "localAuthMode")
		}
	}

	result, err := json.Marshal(rawMap)
	if err != nil {
		return nil, fmt.Errorf("migrateLocalAuthMode: failed to marshal migrated config: %w", err)
	}
	return result, nil
}

func runMigrations(raw json.RawMessage) (json.RawMessage, bool, error) {
	current := raw
	didMigrate := false
	for i, migration := range configMigrations {
		migrated, err := migration(current)
		if err != nil {
			return current, didMigrate, fmt.Errorf("migration %d failed: %w", i, err)
		}
		if string(migrated) != string(current) {
			didMigrate = true
			logger.Info().Int("migration", i).Msg("config migrated")
		}
		current = migrated
	}
	return current, didMigrate, nil
}

func writeRawConfig(path string, raw json.RawMessage) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		return fmt.Errorf("failed to indent config JSON: %w", err)
	}

	if _, err := indented.WriteTo(file); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func LoadConfig() {
	configLock.Lock()
	defer configLock.Unlock()

	if config != nil {
		logger.Debug().Msg("config already loaded, skipping")
		return
	}

	if defaultConfig.UsbConfig.SerialNumber == "" {
		serialNumber, err := extractSerialNumber()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to extract serial number")
		} else {
			defaultConfig.UsbConfig.SerialNumber = serialNumber
		}
	}
	loadedConfig := *defaultConfig
	config = &loadedConfig

	rawData, err := os.ReadFile(configPath)
	if err != nil {
		logger.Debug().Msg("config file does not exist, using default")
		return
	}

	migrated, didMigrate, err := runMigrations(json.RawMessage(rawData))
	if err != nil {
		logger.Warn().Err(err).Msg("config migration failed, preserving corrupt file")
		corruptPath := configPath + ".corrupt"
		_ = os.Rename(configPath, corruptPath)
		logger.Info().Str("corrupt_path", corruptPath).Msg("corrupt config preserved for diagnosis")
		return
	}

	if didMigrate {
		if writeErr := writeRawConfig(configPath, migrated); writeErr != nil {
			logger.Warn().Err(writeErr).Msg("failed to write migrated config, continuing with in-memory version")
		} else {
			logger.Info().Msg("migrated config saved to disk")
			SyncConfigSD(false)
		}
	}

	if err := json.Unmarshal(migrated, &loadedConfig); err != nil {
		logger.Warn().Err(err).Msg("config file JSON parsing failed, preserving corrupt file")
		corruptPath := configPath + ".corrupt"
		_ = os.Rename(configPath, corruptPath)
		logger.Info().Str("corrupt_path", corruptPath).Msg("corrupt config preserved for diagnosis")
		return
	}

	if loadedConfig.UsbConfig == nil {
		loadedConfig.UsbConfig = defaultConfig.UsbConfig
	}

	if loadedConfig.UsbDevices == nil {
		loadedConfig.UsbDevices = defaultConfig.UsbDevices
	}

	if loadedConfig.NetworkConfig == nil {
		loadedConfig.NetworkConfig = defaultConfig.NetworkConfig
	}

	if loadedConfig.Firewall == nil {
		loadedConfig.Firewall = defaultConfig.Firewall
	}

	if loadedConfig.TurnServers == nil {
		loadedConfig.TurnServers = []TurnServer{}
	}

	if loadedConfig.KvmSwitch == nil {
		loadedConfig.KvmSwitch = defaultConfig.KvmSwitch
	} else if loadedConfig.KvmSwitch.PortNames == nil {
		loadedConfig.KvmSwitch.PortNames = defaultConfig.KvmSwitch.PortNames
	}

	config = &loadedConfig

	// Load serial port settings from config
	loadSerialSettingsFromConfig()

	logging.GetRootLogger().UpdateLogLevel(config.DefaultLogLevel)

	logger.Info().Str("path", configPath).Msg("config loaded")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	if err := out.Sync(); err != nil {
		return err
	}

	return nil
}

func SyncConfigSD(isUpdate bool) {
	resp, err := rpcGetSDMountStatus()
	if err != nil {
		logger.Error().Err(err).Msg("failed to get sd mount status")
		return
	}

	if resp.Status == SDMountOK {
		if _, err := os.Stat(configPath); err != nil {
			if err := SaveConfig(); err != nil {
				logger.Error().Err(err).Msg("failed to create kvm_config.json")
				return
			}
		}

		if isUpdate {
			if _, err := os.Stat(sdConfigPath); err == nil {
				if err := copyFile(sdConfigPath, configPath); err != nil {
					logger.Error().Err(err).Msg("failed to copy kvm_config.json from sdcard to userdata")
					return
				}
			} else {
				if err := copyFile(configPath, sdConfigPath); err != nil {
					logger.Error().Err(err).Msg("failed to copy kvm_config.json from userdata to sdcard")
					return
				}
			}
		} else {
			if err := copyFile(configPath, sdConfigPath); err != nil {
				logger.Error().Err(err).Msg("failed to copy kvm_config.json from userdata to sdcard")
				return
			}
		}
	}
}

func SaveConfig() error {
	configLock.Lock()
	defer configLock.Unlock()

	logger.Trace().Str("path", configPath).Msg("Saving config")

	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	SyncConfigSD(false)

	return nil
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func ensureConfigLoaded() {
	if config == nil {
		LoadConfig()
	}
}

var systemInfoWriteLock sync.Mutex

func writeSystemInfoImg() error {
	systemInfoWriteLock.Lock()
	defer systemInfoWriteLock.Unlock()

	imgPath := filepath.Join(imagesFolder, "system_info.img")
	unverifiedimgPath := filepath.Join(imagesFolder, "system_info.img") + ".unverified"
	mountPoint := "/mnt/system_info"

	run := func(cmd string, args ...string) error {
		c := exec.Command(cmd, args...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}

	if _, err := os.Stat(unverifiedimgPath); err == nil {
		err := os.Rename(unverifiedimgPath, imgPath)
		if err != nil {
			return fmt.Errorf("failed to rename %s to %s: %v", unverifiedimgPath, imgPath, err)
		}
		return nil
	}

	isMounted := false
	if f, err := os.Open("/proc/mounts"); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[1] == mountPoint {
				isMounted = true
				break
			}
		}
	}

	if isMounted {
		logger.Info().Msgf("%s is mounted, umounting...\n", mountPoint)
		_ = run("umount", mountPoint)
	}

	if _, err := os.Stat(mountPoint); err == nil {
		if err := os.Remove(mountPoint); err != nil {
			return fmt.Errorf("failed to remove %s: %v", mountPoint, err)
		}
	}

	if _, err := os.Stat(imgPath); err == nil {
		if err := copyFile(imgPath, unverifiedimgPath); err != nil {
			logger.Error().Err(err).Msg("failed to copy system_info.img")
			return err
		}
	} else {
		if err := run("dd", "if=/dev/zero", "of="+unverifiedimgPath, "bs=1M", "count=4"); err != nil {
			return fmt.Errorf("dd failed: %v", err)
		}

		if err := run("mkfs.vfat", unverifiedimgPath); err != nil {
			return fmt.Errorf("mkfs.vfat failed: %v", err)
		}
	}

	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return fmt.Errorf("mkdir failed: %v", err)
	}

	if err := run("mount", "-o", "loop", unverifiedimgPath, mountPoint); err != nil {
		return fmt.Errorf("mount failed: %v", err)
	}

	if err := run("cp", "/etc/hostname", mountPoint+"/hostname.txt"); err != nil {
		return fmt.Errorf("copy hostname failed: %v", err)
	}
	if err := run("sh", "-c", "ip addr show > "+mountPoint+"/network_info.txt"); err != nil {
		return fmt.Errorf("write network info failed: %v", err)
	}

	_ = run("umount", mountPoint)
	if err := os.RemoveAll(mountPoint); err != nil {
		return fmt.Errorf("failed to remove %s: %v", mountPoint, err)
	}

	if err := os.Rename(unverifiedimgPath, imgPath); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %v", unverifiedimgPath, imgPath, err)
	}

	logger.Info().Msg("system_info.img update successfully")
	return nil
}
