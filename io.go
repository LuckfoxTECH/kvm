package kvm

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ioControlMu serialises writes to the power / reset GPIO lines so the web UI
// and the touchscreen cannot drive the same line at the same time.
var ioControlMu sync.Mutex

const (
	gpioBasePath  = "/sys/class/gpio"
	ledGreenPath  = "/sys/class/leds/led:g"
	ledYellowPath = "/sys/class/leds/led:y"
)

func exportGPIO(pin int) error {
	exportFile := gpioBasePath + "/export"
	return os.WriteFile(exportFile, []byte(strconv.Itoa(pin)), 0644)
}

func unexportGPIO(pin int) error {
	unexportFile := gpioBasePath + "/unexport"
	return os.WriteFile(unexportFile, []byte(strconv.Itoa(pin)), 0644)
}

func isGPIOExported(pin int) bool {
	gpioPath := fmt.Sprintf("%s/gpio%d", gpioBasePath, pin)
	_, err := os.Stat(gpioPath)
	return err == nil
}

func setGPIODirection(pin int, direction string) error {
	if !isGPIOExported(pin) {
		if err := exportGPIO(pin); err != nil {
			return fmt.Errorf("failed to export GPIO: %v", err)
		}
	}
	directionFile := fmt.Sprintf("%s/gpio%d/direction", gpioBasePath, pin)
	return os.WriteFile(directionFile, []byte(direction), 0644)
}

func setGPIOValue(pin int, status bool) error {
	var value int
	if status {
		value = 1
	} else {
		value = 0
	}
	if !isGPIOExported(pin) {
		if err := exportGPIO(pin); err != nil {
			return fmt.Errorf("failed to export GPIO: %v", err)
		}
		if err := setGPIODirection(pin, "out"); err != nil {
			return fmt.Errorf("failed to set GPIO direction: %v", err)
		}
	}
	valueFile := fmt.Sprintf("%s/gpio%d/value", gpioBasePath, pin)
	return os.WriteFile(valueFile, []byte(strconv.Itoa(value)), 0644)
}

func setLedMode(ledConfigPath string, mode string) error {
	switch mode {
	case "network-link":
		err := os.WriteFile(ledConfigPath+"/trigger", []byte("netdev"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED trigger: %v", err)
		}
		err = os.WriteFile(ledConfigPath+"/device_name", []byte("eth0"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED device name: %v", err)
		}
		err = os.WriteFile(ledConfigPath+"/link", []byte("1"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED link: %v", err)
		}
	case "network-tx":
		err := os.WriteFile(ledConfigPath+"/trigger", []byte("netdev"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED trigger: %v", err)
		}
		err = os.WriteFile(ledConfigPath+"/device_name", []byte("eth0"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED device name: %v", err)
		}
		err = os.WriteFile(ledConfigPath+"/tx", []byte("1"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED tx: %v", err)
		}
	case "network-rx":
		err := os.WriteFile(ledConfigPath+"/trigger", []byte("netdev"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED trigger: %v", err)
		}
		err = os.WriteFile(ledConfigPath+"/device_name", []byte("eth0"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED device name: %v", err)
		}
		err = os.WriteFile(ledConfigPath+"/rx", []byte("1"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED rx: %v", err)
		}
	case "kernel-activity":
		err := os.WriteFile(ledConfigPath+"/trigger", []byte("activity"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED trigger: %v", err)
		}
	case "disabled":
		err := os.WriteFile(ledConfigPath+"/trigger", []byte("none"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED trigger: %v", err)
		}
		err = os.WriteFile(ledConfigPath+"/brightness", []byte("0"), 0644)
		if err != nil {
			return fmt.Errorf("failed to set LED brightness: %v", err)
		}
	default:
		return fmt.Errorf("invalid LED mode: %s", mode)
	}
	return nil
}

func pulseGPIO(pin int, duration time.Duration) error {
	ioControlMu.Lock()
	defer ioControlMu.Unlock()

	// First pull up
	if err := setGPIOValue(pin, true); err != nil {
		return err
	}

	// Wait for duration
	time.Sleep(duration)

	// Then pull down
	if err := setGPIOValue(pin, false); err != nil {
		return err
	}

	return nil
}

func getGPIOValue(pin int) (bool, error) {
	if !isGPIOExported(pin) {
		if err := exportGPIO(pin); err != nil {
			return false, fmt.Errorf("failed to export GPIO: %v", err)
		}
		if err := setGPIODirection(pin, "in"); err != nil {
			return false, fmt.Errorf("failed to set GPIO direction: %v", err)
		}
	}
	valueFile := fmt.Sprintf("%s/gpio%d/value", gpioBasePath, pin)
	data, err := os.ReadFile(valueFile)
	if err != nil {
		return false, fmt.Errorf("failed to read GPIO value: %v", err)
	}
	value := strings.TrimSpace(string(data))
	return value == "1", nil
}

func resetIOInput() error {
	// Reset IO2 (GPIO0) and IO3 (GPIO1) to input mode
	if err := setGPIODirection(0, "in"); err != nil {
		return fmt.Errorf("failed to reset IO2: %v", err)
	}
	if err := setGPIODirection(1, "in"); err != nil {
		return fmt.Errorf("failed to reset IO3: %v", err)
	}
	return nil
}

func initGPIO() {
	LoadConfig()
	// IO0: GPIO58 IO1: GPIO59
	_ = setGPIOValue(58, config.IO0Status)
	_ = setGPIOValue(59, config.IO1Status)

	// IO2: GPIO0 IO3: GPIO1 - Input
	_ = setGPIODirection(0, "in")
	_ = setGPIODirection(1, "in")

	_ = setLedMode(ledYellowPath, config.LEDYellowMode)
	_ = setLedMode(ledGreenPath, config.LEDGreenMode)
}
