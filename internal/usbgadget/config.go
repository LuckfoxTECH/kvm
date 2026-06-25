package usbgadget

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

type gadgetConfigItem struct {
	order       uint
	device      string
	path        []string
	attrs       gadgetAttributes
	configAttrs gadgetAttributes
	configPath  []string
	reportDesc  []byte
}

type gadgetAttributes map[string]string

type gadgetConfigItemWithKey struct {
	key  string
	item gadgetConfigItem
}

type orderedGadgetConfigItems []gadgetConfigItemWithKey

var defaultGadgetConfig = map[string]gadgetConfigItem{
	"base": {
		order: 0,
		attrs: gadgetAttributes{
			"bcdUSB":    "0x0200", // USB 2.0
			"idVendor":  "0x1d6b", // The Linux Foundation
			"idProduct": "0104",   // Multifunction Composite Gadget
			"bcdDevice": "0100",
		},
		configAttrs: gadgetAttributes{
			"MaxPower":     "250",  // in unit of 2mA
			"bmAttributes": "0xa0", // 0x80 = bus-powered, 0xa0 = bus-powered + remote wakeup
		},
	},
	"base_info": {
		order:      1,
		path:       []string{"strings", "0x409"},
		configPath: []string{"strings", "0x409"},
		attrs: gadgetAttributes{
			"serialnumber": "",
			"manufacturer": "KVM",
			"product":      "KVM USB Emulation Device",
		},
		configAttrs: gadgetAttributes{
			"configuration": "Config 1: HID",
		},
	},
	// mtp
	"mtp": mtpConfig,
	// keyboard HID
	"keyboard": keyboardConfig,
	// mouse HID
	"absolute_mouse": absoluteMouseConfig,
	// relative mouse HID
	"relative_mouse": relativeMouseConfig,
	// mass storage
	"mass_storage_base": massStorageBaseConfig,
	"mass_storage_lun0": massStorageLun0Config,
	// audio
	"audio": {
		order:      4000,
		device:     "uac1.usb0",
		path:       []string{"functions", "uac1.usb0"},
		configPath: []string{"uac1.usb0"},
		attrs: gadgetAttributes{
			"p_chmask":         "3",
			"p_srate":          "48000",
			"p_ssize":          "2",
			"p_volume_present": "0",
			"c_chmask":         "3",
			"c_srate":          "48000",
			"c_ssize":          "2",
			"c_volume_present": "0",
		},
	},
}

func (u *UsbGadget) isGadgetConfigItemEnabled(itemKey string) bool {
	switch itemKey {
	case "absolute_mouse":
		return u.enabledDevices.AbsoluteMouse
	case "relative_mouse":
		return u.enabledDevices.RelativeMouse
	case "keyboard":
		return u.enabledDevices.Keyboard
	case "mass_storage_base":
		return u.enabledDevices.MassStorage
	case "mass_storage_lun0":
		return u.enabledDevices.MassStorage
	case "mtp":
		return u.enabledDevices.Mtp
	case "audio":
		return u.enabledDevices.Audio
	default:
		return true
	}
}

func (u *UsbGadget) loadGadgetConfig() {
	if u.customConfig.isEmpty {
		u.log.Trace().Msg("using default gadget config")
		return
	}

	u.configMap["base"].attrs["idVendor"] = u.customConfig.VendorId
	u.configMap["base"].attrs["idProduct"] = u.customConfig.ProductId

	u.configMap["base_info"].attrs["serialnumber"] = u.customConfig.SerialNumber
	u.configMap["base_info"].attrs["manufacturer"] = u.customConfig.Manufacturer
	u.configMap["base_info"].attrs["product"] = u.customConfig.Product
}

func (u *UsbGadget) SetGadgetConfig(config *Config) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	if config == nil {
		return // nothing to do
	}

	u.customConfig = *config
	u.loadGadgetConfig()
}

func (u *UsbGadget) SetGadgetDevices(devices *Devices) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	if devices == nil {
		return // nothing to do
	}

	u.enabledDevices = *devices
}

// GetConfigPath returns the path to the config item.
func (u *UsbGadget) GetConfigPath(itemKey string) (string, error) {
	item, ok := u.configMap[itemKey]
	if !ok {
		return "", fmt.Errorf("config item %s not found", itemKey)
	}
	return joinPath(u.kvmGadgetPath, item.configPath), nil
}

// GetPath returns the path to the item.
func (u *UsbGadget) GetPath(itemKey string) (string, error) {
	item, ok := u.configMap[itemKey]
	if !ok {
		return "", fmt.Errorf("config item %s not found", itemKey)
	}
	return joinPath(u.kvmGadgetPath, item.path), nil
}

// OverrideGadgetConfig overrides the gadget config for the given item and attribute.
// It returns an error if the item is not found or the attribute is not found.
// It returns true if the attribute is overridden, false otherwise.
func (u *UsbGadget) OverrideGadgetConfig(itemKey string, itemAttr string, value string) (error, bool) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	// get it as a pointer
	_, ok := u.configMap[itemKey]
	if !ok {
		return fmt.Errorf("config item %s not found", itemKey), false
	}

	if u.configMap[itemKey].attrs[itemAttr] == value {
		return nil, false
	}

	u.configMap[itemKey].attrs[itemAttr] = value
	u.log.Info().Str("itemKey", itemKey).Str("itemAttr", itemAttr).Str("value", value).Msg("overriding gadget config")

	return nil, true
}

func mountConfigFS(path string) error {
	err := exec.Command("mount", "-t", "configfs", "none", path).Run()
	if err != nil {
		return fmt.Errorf("failed to mount configfs: %w", err)
	}
	return nil
}

func mountFunctionFS(path string) error {
	err := os.MkdirAll("/dev/ffs-mtp", 0755)
	if err != nil {
		return fmt.Errorf("failed to create mtp dev dir: %w", err)
	}
	mounted := false
	if f, err := os.Open("/proc/mounts"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), functionFSPath) {
				mounted = true
				break
			}
		}
		f.Close()
	}

	if !mounted {
		err := exec.Command("mount", "-t", "functionfs", "mtp", path).Run()
		if err != nil {
			return fmt.Errorf("failed to mount functionfs: %w", err)
		}
	}

	umtprdRunning := false
	if out, err := exec.Command("pgrep", "-x", "umtprd").Output(); err == nil && len(out) > 0 {
		umtprdRunning = true
	}

	if !umtprdRunning {
		cmd := exec.Command("umtprd")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to exec binary: %w", err)
		}
	}

	return nil
}

func (u *UsbGadget) Init() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	u.loadGadgetConfig()

	udcs := getUdcs()
	if len(udcs) < 1 {
		u.log.Warn().Msg("no UDC found, skipping USB stack init")
		return u.logWarn("no udc found, skipping USB stack init", nil)
	}

	u.udc = udcs[0]

	if err := u.ensureGadgetUnbound(); err != nil {
		u.log.Warn().Err(err).Msg("failed to ensure gadget is unbound, will continue")
	} else {
		u.log.Info().Msg("gadget unbind check completed")
	}

	err := u.configureUsbGadget(false)
	if err != nil {
		u.log.Error().Err(err).
			Str("udc", u.udc).
			Interface("enabled_devices", u.enabledDevices).
			Msg("USB gadget initialization FAILED")
		return u.logError("unable to initialize USB stack", err)
	}

	return nil
}

func (u *UsbGadget) ensureGadgetUnbound() error {
	udcPath := path.Join(u.kvmGadgetPath, "UDC")

	if _, err := os.Stat(u.kvmGadgetPath); os.IsNotExist(err) {
		return nil
	}

	udcContent, err := os.ReadFile(udcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read UDC file: %w", err)
	}

	currentUDC := strings.TrimSpace(string(udcContent))
	if currentUDC == "" || currentUDC == "none" {
		return nil
	}

	u.log.Info().
		Str("current_udc", currentUDC).
		Str("target_udc", u.udc).
		Msg("unbinding existing UDC before reconfiguration")

	if err := u.UnbindUDC(); err != nil {
		u.log.Warn().Err(err).Msg("failed to unbind via UDC file, trying DWC3")
		if err := u.UnbindUDCToDWC3(); err != nil {
			return fmt.Errorf("failed to unbind UDC: %w", err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	if content, err := os.ReadFile(udcPath); err == nil {
		if strings.TrimSpace(string(content)) != "none" {
			u.log.Warn().Msg("UDC still bound after unbind attempt")
		}
	}

	return nil
}

func (u *UsbGadget) UpdateGadgetConfig() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	u.loadGadgetConfig()
	u.CloseHidFiles()

	err := u.configureUsbGadget(true)
	if err != nil {
		return u.logError("unable to update gadget config", err)
	}

	return nil
}

func (u *UsbGadget) configureUsbGadget(resetUsb bool) error {
	u.log.Info().
		Bool("reset_usb", resetUsb).
		Msg("configuring USB gadget via transaction")

	return u.WithTransaction(func() error {
		u.log.Info().Msg("Transaction: Mounting configfs")
		u.tx.MountConfigFS()

		u.log.Info().Msg("Transaction: Creating config path")
		u.tx.CreateConfigPath()

		u.log.Info().Msg("Transaction: Writing gadget configuration")
		u.tx.WriteGadgetConfig()

		if resetUsb {
			u.log.Info().Msg("Transaction: Rebinding USB")
			u.tx.RebindUsb(true)
		}
		return nil
	})
}

func (u *UsbGadget) VerifyMassStorage() error {
	if !u.enabledDevices.MassStorage {
		return nil
	}

	massStoragePath := path.Join(u.kvmGadgetPath, "functions/mass_storage.usb0")
	if _, err := os.Stat(massStoragePath); err != nil {
		return fmt.Errorf("mass_storage function not found: %w", err)
	}

	lunPath := path.Join(massStoragePath, "lun.0")
	if _, err := os.Stat(lunPath); err != nil {
		return fmt.Errorf("mass_storage LUN not found: %w", err)
	}

	configLink := path.Join(u.configC1Path, "mass_storage.usb0")
	if _, err := os.Lstat(configLink); err != nil {
		return fmt.Errorf("mass_storage symlink not found: %w", err)
	}

	u.log.Info().Msg("mass storage verified")
	return nil
}
