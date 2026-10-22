package kvm

import (
	"fmt"
	"os"
	"strings"

	"kvm/internal/network"
	"kvm/internal/udhcpc"

	"github.com/guregu/null/v6"
)

const (
	NetIfName = "eth0"
)

var (
	networkState *network.NetworkInterfaceState
)

func setProxyEnvVar(key, value string) {
	value = strings.TrimSpace(value)
	upperKey := strings.ToUpper(key)
	if value == "" {
		_ = os.Unsetenv(key)
		_ = os.Unsetenv(upperKey)
		return
	}
	_ = os.Setenv(key, value)
	_ = os.Setenv(upperKey, value)
}

func applyProxyEnvironment(networkConfig *network.NetworkConfig) {
	if networkConfig == nil {
		return
	}
	setProxyEnvVar("http_proxy", networkConfig.HTTPProxy.String)
	setProxyEnvVar("https_proxy", networkConfig.HTTPSProxy.String)
	setProxyEnvVar("all_proxy", networkConfig.ALLProxy.String)
}

func networkStateChanged() {
	// do not block the main thread
	go waitCtrlAndRequestDisplayUpdate(true)

	// always restart mDNS when the network state changes
	if mDNS != nil {
		_ = mDNS.SetListenOptions(config.NetworkConfig.GetMDNSMode())
		_ = mDNS.SetLocalNames([]string{
			networkState.GetHostname(),
			networkState.GetFQDN(),
		}, true)
	}
}

func initNetwork() error {
	ensureConfigLoaded()
	applyProxyEnvironment(config.NetworkConfig)

	state, err := network.NewNetworkInterfaceState(&network.NetworkInterfaceOptions{
		DefaultHostname: GetDefaultHostname(),
		InterfaceName:   NetIfName,
		NetworkConfig:   config.NetworkConfig,
		Logger:          networkLogger,
		OnStateChange: func(state *network.NetworkInterfaceState) {
			networkStateChanged()
		},
		OnInitialCheck: func(state *network.NetworkInterfaceState) {
			networkStateChanged()
		},
		OnDhcpLeaseChange: func(lease *udhcpc.Lease) {
			networkStateChanged()

			if currentSession == nil {
				return
			}

			writeJSONRPCEvent("networkState", networkState.RpcGetNetworkState(), currentSession)
		},
		OnConfigChange: func(networkConfig *network.NetworkConfig) {
			config.NetworkConfig = networkConfig
			config.AppliedNetworkConfig = networkConfig
			networkStateChanged()
		},
	})

	if state == nil {
		if err == nil {
			return fmt.Errorf("failed to create NetworkInterfaceState")
		}
		return err
	}

	if err := state.Run(); err != nil {
		return err
	}

	networkState = state

	if config != nil && config.NetworkConfig != nil {
		if config.NetworkConfig.PendingReboot.Valid && config.NetworkConfig.PendingReboot.Bool {
			if config.AppliedNetworkConfig != nil && network.IsSame(config.AppliedNetworkConfig, *config.NetworkConfig) {
				config.NetworkConfig.PendingReboot = null.BoolFrom(false)
				_ = SaveConfig()
			}
		}
	}

	if config != nil && config.NetworkConfig != nil {
		if config.NetworkConfig.PendingReboot.Valid && config.NetworkConfig.PendingReboot.Bool {
			config.NetworkConfig.PendingReboot = null.BoolFrom(false)
			_ = SaveConfig()
		}
	}

	return nil
}

func rpcGetNetworkState() network.RpcNetworkState {
	return networkState.RpcGetNetworkState()
}

func rpcGetNetworkSettings() network.RpcNetworkSettings {
	rpcSettings := networkState.RpcGetNetworkSettings()
	hostname := GetHostname()
	rpcSettings.Hostname = null.NewString(hostname, hostname != "")
	return rpcSettings
}

func rpcSetNetworkSettings(settings network.RpcNetworkSettings) (*network.RpcNetworkSettings, error) {
	current := networkState.RpcGetNetworkSettings()
	changedCore := !network.IsSame(current.NetworkConfig, settings.NetworkConfig)
	if changedCore {
		settings.NetworkConfig.PendingReboot = null.BoolFrom(true)
	}

	s := networkState.RpcSetNetworkSettings(settings)
	if s != nil {
		return nil, s
	}
	applied := networkState.RpcGetNetworkSettings()
	config.NetworkConfig = &applied.NetworkConfig
	// If we just reverted to the same core config as applied, clear pending_reboot
	if config.AppliedNetworkConfig != nil {
		// create copies ignoring PendingReboot
		a := *config.AppliedNetworkConfig
		b := applied.NetworkConfig
		a.PendingReboot = null.Bool{}
		b.PendingReboot = null.Bool{}
		if network.IsSame(a, b) {
			config.NetworkConfig.PendingReboot = null.BoolFrom(false)
		}
	}
	if err := SaveConfig(); err != nil {
		return nil, err
	}
	applyProxyEnvironment(config.NetworkConfig)

	return &network.RpcNetworkSettings{NetworkConfig: *config.NetworkConfig}, nil
}

func rpcRenewDHCPLease() error {
	return networkState.RpcRenewDHCPLease()
}

// rpcRestartDHCPClient drops the current lease and runs a fresh DHCP discover.
func rpcRestartDHCPClient() error {
	return networkState.RpcRestartDHCPClient()
}

func rpcRequestDHCPAddress(ip string) error {
	return networkState.RpcRequestDHCPAddress(ip)
}

const ethernetMacAddressPath = "/userdata/ethaddr.txt"

func rpcSetEthernetMacAddress(macAddress string) (interface{}, error) {
	normalized, err := networkState.SetMACAddress(macAddress)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(ethernetMacAddressPath, []byte(normalized+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", ethernetMacAddressPath, err)
	}
	return networkState.RpcGetNetworkState(), nil
}
