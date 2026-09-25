package kvm

import (
	"encoding/json"
	"testing"
)

func TestKvmSwitchConfigPersistence(t *testing.T) {
	config := &Config{
		KvmSwitch: &KvmSwitchConfig{
			Enabled:         true,
			SwitchIP:        "192.168.1.100",
			SwitchPort:      5000,
			TotalPorts:      4,
			PortNames:       map[int]string{1: "Server 1"},
			HotkeysEnabled:  true,
			PollIntervalSec: 2,
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	var loadedConfig Config
	err = json.Unmarshal(data, &loadedConfig)
	if err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	if loadedConfig.KvmSwitch == nil {
		t.Fatalf("KvmSwitch config is nil")
	}

	if loadedConfig.KvmSwitch.SwitchIP != "192.168.1.100" {
		t.Errorf("Expected IP 192.168.1.100, got %s", loadedConfig.KvmSwitch.SwitchIP)
	}
}
