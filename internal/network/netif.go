package network

import (
    "fmt"
    "net"
    "os"
    "strconv"
    "strings"
    "sync"

    "kvm/internal/confparser"
    "kvm/internal/logging"
    "kvm/internal/udhcpc"

	"github.com/rs/zerolog"

	"github.com/vishvananda/netlink"
)

type NetworkInterfaceState struct {
	interfaceName string
	interfaceUp   bool
	ipv4Addr      *net.IP
	ipv4Addresses []string
	ipv6Addr      *net.IP
	ipv6Addresses []IPv6Address
	ipv6LinkLocal *net.IP
	macAddr       *net.HardwareAddr

	l         *zerolog.Logger
	stateLock sync.Mutex

	config     *NetworkConfig
	dhcpClient *udhcpc.DHCPClient

	defaultHostname string
	currentHostname string
	currentFqdn     string

	onStateChange  func(state *NetworkInterfaceState)
	onInitialCheck func(state *NetworkInterfaceState)
	cbConfigChange func(config *NetworkConfig)

	checked bool
}

type NetworkInterfaceOptions struct {
	InterfaceName     string
	DhcpPidFile       string
	Logger            *zerolog.Logger
	DefaultHostname   string
	OnStateChange     func(state *NetworkInterfaceState)
	OnInitialCheck    func(state *NetworkInterfaceState)
	OnDhcpLeaseChange func(lease *udhcpc.Lease)
	OnConfigChange    func(config *NetworkConfig)
	NetworkConfig     *NetworkConfig
}

func NewNetworkInterfaceState(opts *NetworkInterfaceOptions) (*NetworkInterfaceState, error) {
	if opts.NetworkConfig == nil {
		return nil, fmt.Errorf("NetworkConfig can not be nil")
	}

	if opts.DefaultHostname == "" {
		opts.DefaultHostname = "picokvm"
	}

	err := confparser.SetDefaultsAndValidate(opts.NetworkConfig)
	if err != nil {
		return nil, err
	}

	l := opts.Logger
	s := &NetworkInterfaceState{
		interfaceName:   opts.InterfaceName,
		defaultHostname: opts.DefaultHostname,
		stateLock:       sync.Mutex{},
		l:               l,
		onStateChange:   opts.OnStateChange,
		onInitialCheck:  opts.OnInitialCheck,
		cbConfigChange:  opts.OnConfigChange,
		config:          opts.NetworkConfig,
	}

	// create the dhcp client
	dhcpClient := udhcpc.NewDHCPClient(&udhcpc.DHCPClientOptions{
		InterfaceName: opts.InterfaceName,
		PidFile:       opts.DhcpPidFile,
		Logger:        l,
		OnLeaseChange: func(lease *udhcpc.Lease) {
			_, err := s.update()
			if err != nil {
				opts.Logger.Error().Err(err).Msg("failed to update network state")
				return
			}

			_ = s.setHostnameIfNotSame()

			opts.OnDhcpLeaseChange(lease)
		},
		RequestAddress: s.config.IPv4RequestAddress.String,
	})

	s.dhcpClient = dhcpClient

	mode := strings.TrimSpace(s.config.IPv4Mode.String)
	switch mode {
	case "static":
		if s.dhcpClient != nil {
			s.dhcpClient.SetEnabled(false)
		}
		if err := s.applyIPv4Static(); err != nil {
			l.Error().Err(err).Msg("failed to apply static IPv4 on init")
		}
	case "dhcp":
		if s.dhcpClient != nil {
			s.dhcpClient.SetEnabled(true)
		}
	case "disabled":
		if s.dhcpClient != nil {
			s.dhcpClient.SetEnabled(false)
		}
		if err := s.clearIPv4Addresses(); err != nil {
			l.Warn().Err(err).Msg("failed to clear IPv4 addresses on init")
		}
		if err := s.clearDefaultIPv4Route(); err != nil {
			l.Debug().Err(err).Msg("failed to clear default route on init")
		}
	}

	return s, nil
}

func (s *NetworkInterfaceState) IsUp() bool {
	return s.interfaceUp
}

func (s *NetworkInterfaceState) HasIPAssigned() bool {
	return s.ipv4Addr != nil || s.ipv6Addr != nil
}

func (s *NetworkInterfaceState) IsOnline() bool {
	return s.IsUp() && s.HasIPAssigned()
}

func (s *NetworkInterfaceState) IPv4() *net.IP {
	return s.ipv4Addr
}

func (s *NetworkInterfaceState) IPv4String() string {
	if s.ipv4Addr == nil {
		return "..."
	}
	return s.ipv4Addr.String()
}

func (s *NetworkInterfaceState) IPv6() *net.IP {
	return s.ipv6Addr
}

func (s *NetworkInterfaceState) IPv6String() string {
	if s.ipv6Addr == nil {
		return "..."
	}
	return s.ipv6Addr.String()
}

func (s *NetworkInterfaceState) MAC() *net.HardwareAddr {
	return s.macAddr
}

func (s *NetworkInterfaceState) MACString() string {
	if s.macAddr == nil {
		return ""
	}
	return s.macAddr.String()
}

func (s *NetworkInterfaceState) SetMACAddress(macAddress string) (string, error) {
	macAddress = strings.TrimSpace(macAddress)
	if macAddress == "" {
		return "", fmt.Errorf("mac address is empty")
	}
	hw, err := net.ParseMAC(macAddress)
	if err != nil {
		return "", fmt.Errorf("invalid mac address")
	}
	if len(hw) != 6 {
		return "", fmt.Errorf("invalid mac address length")
	}
	normalized := strings.ToLower(hw.String())

	s.stateLock.Lock()
	iface, err := netlink.LinkByName(s.interfaceName)
	if err != nil {
		s.stateLock.Unlock()
		return "", err
	}
	if err := netlink.LinkSetDown(iface); err != nil {
		s.stateLock.Unlock()
		return "", err
	}
	if err := netlink.LinkSetHardwareAddr(iface, hw); err != nil {
		s.stateLock.Unlock()
		return "", err
	}
	if err := netlink.LinkSetUp(iface); err != nil {
		s.stateLock.Unlock()
		return "", err
	}
	s.stateLock.Unlock()

	switch strings.TrimSpace(s.config.IPv4Mode.String) {
	case "dhcp":
		if s.dhcpClient != nil {
			_ = s.dhcpClient.Renew()
		}
	case "static":
		// cycling the link drops the addresses, so reapply the static config
		if err := s.applyIPv4Static(); err != nil {
			s.l.Error().Err(err).Msg("failed to reapply static IPv4 after MAC change")
		}
	}
	if _, err := s.update(); err != nil {
		return normalized, err
	}
	return normalized, nil
}

func (s *NetworkInterfaceState) update() (DhcpTargetState, error) {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	dhcpTargetState := DhcpTargetStateDoNothing

	iface, err := netlink.LinkByName(s.interfaceName)
	if err != nil {
		s.l.Error().Err(err).Msg("failed to get interface")
		return dhcpTargetState, err
	}

	// detect if the interface status changed
	var changed bool
	attrs := iface.Attrs()
	state := attrs.OperState
	newInterfaceUp := state == netlink.OperUp

	// check if the interface is coming up
	interfaceGoingUp := !s.interfaceUp && newInterfaceUp
	interfaceGoingDown := s.interfaceUp && !newInterfaceUp

	if s.interfaceUp != newInterfaceUp {
		s.interfaceUp = newInterfaceUp
		changed = true
	}

	if changed {
		if interfaceGoingUp {
			s.l.Info().Msg("interface state transitioned to up")
			if s.isStaticIPv4() {
				dhcpTargetState = DhcpTargetStateApplyStaticIPv4
			} else {
				dhcpTargetState = DhcpTargetStateRenew
			}
		} else if interfaceGoingDown {
			s.l.Info().Msg("interface state transitioned to down")
		}
	}

	// set the mac address
	s.macAddr = &attrs.HardwareAddr

	// get the ip addresses
	addrs, err := netlinkAddrs(iface)
	if err != nil {
		return dhcpTargetState, logging.ErrorfL(s.l, "failed to get ip addresses", err)
	}

	var (
		ipv4Addresses       = make([]net.IP, 0)
		ipv4AddressesString = make([]string, 0)
		ipv6Addresses       = make([]IPv6Address, 0)
		// ipv6AddressesString = make([]string, 0)
		ipv6LinkLocal *net.IP
	)

	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			scopedLogger := s.l.With().Str("ipv4", addr.IP.String()).Logger()
			if interfaceGoingDown {
				// remove all IPv4 addresses from the interface.
				scopedLogger.Info().Msg("state transitioned to down, removing IPv4 address")
				err := netlink.AddrDel(iface, &addr)
				if err != nil {
					scopedLogger.Warn().Err(err).Msg("failed to delete address")
				}
				// notify the DHCP client to release the lease
				dhcpTargetState = DhcpTargetStateRelease
				continue
			}
			ipv4Addresses = append(ipv4Addresses, addr.IP)
			ipv4AddressesString = append(ipv4AddressesString, addr.IPNet.String())
		} else if addr.IP.To16() != nil {
			scopedLogger := s.l.With().Str("ipv6", addr.IP.String()).Logger()
			// check if it's a link local address
			if addr.IP.IsLinkLocalUnicast() {
				ipv6LinkLocal = &addr.IP
				continue
			}

			if !addr.IP.IsGlobalUnicast() {
				scopedLogger.Trace().Msg("not a global unicast address, skipping")
				continue
			}

			if interfaceGoingDown {
				scopedLogger.Info().Msg("state transitioned to down, removing IPv6 address")
				err := netlink.AddrDel(iface, &addr)
				if err != nil {
					scopedLogger.Warn().Err(err).Msg("failed to delete address")
				}
				continue
			}
			ipv6Addresses = append(ipv6Addresses, IPv6Address{
				Address:           addr.IP,
				Prefix:            *addr.IPNet,
				ValidLifetime:     lifetimeToTime(addr.ValidLft),
				PreferredLifetime: lifetimeToTime(addr.PreferedLft),
				Scope:             addr.Scope,
			})
			// ipv6AddressesString = append(ipv6AddressesString, addr.IPNet.String())
		}
	}

	if len(ipv4Addresses) > 0 {
		// compare the addresses to see if there's a change
		if s.ipv4Addr == nil || s.ipv4Addr.String() != ipv4Addresses[0].String() {
			scopedLogger := s.l.With().Str("ipv4", ipv4Addresses[0].String()).Logger()
			if s.ipv4Addr != nil {
				scopedLogger.Info().
					Str("old_ipv4", s.ipv4Addr.String()).
					Msg("IPv4 address changed")
			} else {
				scopedLogger.Info().Msg("IPv4 address found")
			}
			s.ipv4Addr = &ipv4Addresses[0]
			changed = true
		}
	} else if s.ipv4Addr != nil {
		// No address left on the interface: drop the stale pointer instead of
		// keeping reporting an address that no longer exists.
		s.l.Warn().Str("old_ipv4", s.ipv4Addr.String()).Msg("IPv4 address removed")
		s.ipv4Addr = nil
		changed = true
	}
	s.ipv4Addresses = ipv4AddressesString

	// A static address can vanish without an up/down transition being observed
	// (replug races, NIC reset, an external `ip addr flush`). Reapply as soon as it
	// is noticed, otherwise the device stays unreachable until the next reboot.
	if newInterfaceUp && !interfaceGoingUp && s.isStaticIPv4() && !s.staticAddressPresent(ipv4AddressesString) {
		s.l.Warn().Msg("configured static IPv4 address is missing, reapplying")
		dhcpTargetState = DhcpTargetStateApplyStaticIPv4
	}

	if ipv6LinkLocal != nil {
		if s.ipv6LinkLocal == nil || s.ipv6LinkLocal.String() != ipv6LinkLocal.String() {
			scopedLogger := s.l.With().Str("ipv6", ipv6LinkLocal.String()).Logger()
			if s.ipv6LinkLocal != nil {
				scopedLogger.Info().
					Str("old_ipv6", s.ipv6LinkLocal.String()).
					Msg("IPv6 link local address changed")
			} else {
				scopedLogger.Info().Msg("IPv6 link local address found")
			}
			s.ipv6LinkLocal = ipv6LinkLocal
			changed = true
		}
	} else if s.ipv6LinkLocal != nil {
		s.l.Warn().Str("old_ipv6", s.ipv6LinkLocal.String()).Msg("IPv6 link local address removed")
		s.ipv6LinkLocal = nil
		changed = true
	}
	// Sort first so the primary IPv6 address picked below stays stable across
	// polls instead of following whatever order netlink returns.
	sortIPv6Addresses(ipv6Addresses)

	if !isSameIPv6AddressList(s.ipv6Addresses, ipv6Addresses) {
		s.l.Info().
			Strs("ipv6_addresses", ipv6AddressStrings(ipv6Addresses)).
			Msg("IPv6 addresses changed")
		changed = true
	}
	s.ipv6Addresses = ipv6Addresses

	if len(ipv6Addresses) > 0 {
		// compare the addresses to see if there's a change
		if s.ipv6Addr == nil || s.ipv6Addr.String() != ipv6Addresses[0].Address.String() {
			scopedLogger := s.l.With().Str("ipv6", ipv6Addresses[0].Address.String()).Logger()
			if s.ipv6Addr != nil {
				scopedLogger.Info().
					Str("old_ipv6", s.ipv6Addr.String()).
					Msg("IPv6 address changed")
			} else {
				scopedLogger.Info().Msg("IPv6 address found")
			}
			s.ipv6Addr = &ipv6Addresses[0].Address
			changed = true
		}
	} else if s.ipv6Addr != nil {
		// No global address left on the interface: drop the stale pointer instead
		// of keeping reporting an address that no longer exists.
		s.l.Warn().Str("old_ipv6", s.ipv6Addr.String()).Msg("IPv6 address removed")
		s.ipv6Addr = nil
		changed = true
	}

	// if it's the initial check, we'll set changed to false
	initialCheck := !s.checked
	if initialCheck {
		s.checked = true
		changed = false
		if dhcpTargetState == DhcpTargetStateRenew || dhcpTargetState == DhcpTargetStateApplyStaticIPv4 {
			// it's the initial check, we'll start the DHCP client
			// dhcpTargetState = DhcpTargetStateStart
			// TODO: manage DHCP client start/stop
			// the static IPv4 config was already applied during initialization
			dhcpTargetState = DhcpTargetStateDoNothing
		}
	}

	if initialCheck {
		s.onInitialCheck(s)
	} else if changed {
		s.onStateChange(s)
	}

	return dhcpTargetState, nil
}

func (s *NetworkInterfaceState) CheckAndUpdateDhcp() error {
	dhcpTargetState, err := s.update()
	if err != nil {
		return logging.ErrorfL(s.l, "failed to update network state", err)
	}

	switch dhcpTargetState {
	case DhcpTargetStateRenew:
		s.l.Info().Msg("renewing DHCP lease")
		_ = s.dhcpClient.Renew()
	case DhcpTargetStateRelease:
		s.l.Info().Msg("releasing DHCP lease")
		_ = s.dhcpClient.Release()
	case DhcpTargetStateApplyStaticIPv4:
		s.l.Info().Msg("reapplying static IPv4")
		if err := s.applyIPv4Static(); err != nil {
			s.l.Error().Err(err).Msg("failed to reapply static IPv4")
		}
	case DhcpTargetStateStart:
		s.l.Warn().Msg("dhcpTargetStateStart not implemented")
	case DhcpTargetStateStop:
		s.l.Warn().Msg("dhcpTargetStateStop not implemented")
	}

	return nil
}

func (s *NetworkInterfaceState) onConfigChange(config *NetworkConfig) {
    _ = s.setHostnameIfNotSame()
    s.cbConfigChange(config)
}

// applyIPv4Mode applies the configured IPv4 mode (dhcp/static/disabled) at runtime.
func (s *NetworkInterfaceState) applyIPv4Mode() error {
	if s.config == nil {
		return nil
	}

	switch strings.TrimSpace(s.config.IPv4Mode.String) {
	case "static":
		if s.dhcpClient != nil {
			s.dhcpClient.SetEnabled(false)
		}
		return s.applyIPv4Static()
	case "dhcp":
		// drop leftover static addresses before letting DHCP take over
		if err := s.clearIPv4Addresses(); err != nil {
			s.l.Warn().Err(err).Msg("failed clearing IPv4 addresses prior to dhcp apply")
		}
		if err := s.clearDefaultIPv4Route(); err != nil {
			s.l.Warn().Err(err).Msg("failed clearing default route prior to dhcp apply")
		}
		if s.dhcpClient != nil {
			s.dhcpClient.SetEnabled(true)
			if reqAddr := strings.TrimSpace(s.config.IPv4RequestAddress.String); reqAddr != "" {
				_ = s.dhcpClient.RequestAddress(reqAddr)
			} else {
				_ = s.dhcpClient.Renew()
			}
		}
		return nil
	case "disabled":
		if s.dhcpClient != nil {
			s.dhcpClient.SetEnabled(false)
			_ = s.dhcpClient.Release()
		}
		if err := s.clearIPv4Addresses(); err != nil {
			s.l.Warn().Err(err).Msg("failed clearing IPv4 addresses for disabled mode")
		}
		if err := s.clearDefaultIPv4Route(); err != nil {
			s.l.Warn().Err(err).Msg("failed clearing default route for disabled mode")
		}
		return nil
	}

	return nil
}

// isStaticIPv4 reports whether IPv4 is configured in static mode.
// Callers inside update() already hold stateLock, so this must not lock.
func (s *NetworkInterfaceState) isStaticIPv4() bool {
	return s.config != nil && strings.TrimSpace(s.config.IPv4Mode.String) == "static"
}

// staticAddressPresent reports whether the configured static IPv4 address is
// currently assigned to the interface. Entries are CIDR strings ("10.0.0.5/24").
func (s *NetworkInterfaceState) staticAddressPresent(addresses []string) bool {
	if s.config == nil || s.config.IPv4Static == nil {
		return true
	}

	wantIP := net.ParseIP(strings.TrimSpace(s.config.IPv4Static.Address.String))
	if wantIP == nil {
		return true
	}

	for _, addrStr := range addresses {
		ip, _, err := net.ParseCIDR(addrStr)
		if err != nil {
			ip = net.ParseIP(addrStr)
		}
		if ip != nil && ip.Equal(wantIP) {
			return true
		}
	}

	return false
}

// clearIPv4Addresses removes all IPv4 addresses from the interface.
func (s *NetworkInterfaceState) clearIPv4Addresses() error {
    iface, err := netlink.LinkByName(s.interfaceName)
    if err != nil {
        return err
    }
    addrs, err := netlinkAddrs(iface)
    if err != nil {
        return err
    }
    for _, addr := range addrs {
        if addr.IP.To4() != nil {
            if err := netlink.AddrDel(iface, &addr); err != nil {
                s.l.Warn().Err(err).Str("addr", addr.IPNet.String()).Msg("failed to delete IPv4 address")
            }
        }
    }
    return nil
}

// clearDefaultIPv4Route removes existing default IPv4 route on this interface.
func (s *NetworkInterfaceState) clearDefaultIPv4Route() error {
    iface, err := netlink.LinkByName(s.interfaceName)
    if err != nil {
        return err
    }
    routes, err := netlink.RouteList(iface, netlink.FAMILY_V4)
    if err != nil {
        return err
    }
    for _, r := range routes {
        if r.Dst == nil { // default route
            if err := netlink.RouteDel(&r); err != nil {
                s.l.Warn().Err(err).Msg("failed to delete default route")
            }
        }
    }
    return nil
}

// parseIPv4Mask converts dotted decimal netmask to net.IPMask.
func parseIPv4Mask(mask string) (net.IPMask, error) {
    parts := strings.Split(strings.TrimSpace(mask), ".")
    if len(parts) != 4 {
        return nil, fmt.Errorf("invalid netmask: %s", mask)
    }
    bytes := make([]byte, 4)
    for i := 0; i < 4; i++ {
        v, err := strconv.Atoi(parts[i])
        if err != nil || v < 0 || v > 255 {
            return nil, fmt.Errorf("invalid netmask octet: %s", parts[i])
        }
        bytes[i] = byte(v)
    }
    return net.IPv4Mask(bytes[0], bytes[1], bytes[2], bytes[3]), nil
}

// writeResolvConf writes DNS servers and optional domain into /etc/resolv.conf.
func (s *NetworkInterfaceState) writeResolvConf(dns []string, domain string) error {
    var b strings.Builder
    if domain != "" {
        b.WriteString("search ")
        b.WriteString(domain)
        b.WriteString("\n")
    }
    for _, d := range dns {
        d = strings.TrimSpace(d)
        if d == "" {
            continue
        }
        b.WriteString("nameserver ")
        b.WriteString(d)
        b.WriteString("\n")
    }
    content := b.String()
    if content == "" {
        return nil
    }
    if err := os.WriteFile("/etc/resolv.conf", []byte(content), 0644); err != nil {
        return err
    }
    s.l.Info().Msg("updated /etc/resolv.conf for static IPv4")
    return nil
}

// applyIPv4Static sets a static IPv4 address, default route, and DNS.
func (s *NetworkInterfaceState) applyIPv4Static() error {
    if s.config == nil || s.config.IPv4Static == nil {
        return fmt.Errorf("IPv4Static config not provided")
    }
    ipStr := strings.TrimSpace(s.config.IPv4Static.Address.String)
    maskStr := strings.TrimSpace(s.config.IPv4Static.Netmask.String)
    gwStr := strings.TrimSpace(s.config.IPv4Static.Gateway.String)
    dns := s.config.IPv4Static.DNS

    ip := net.ParseIP(ipStr)
    if ip == nil || ip.To4() == nil {
        return fmt.Errorf("invalid IPv4 address: %s", ipStr)
    }
    mask, err := parseIPv4Mask(maskStr)
    if err != nil {
        return err
    }

    iface, err := netlink.LinkByName(s.interfaceName)
    if err != nil {
        return err
    }

    // Clear existing IPv4 addresses and default route
    if err := s.clearIPv4Addresses(); err != nil {
        s.l.Warn().Err(err).Msg("failed clearing IPv4 addresses prior to static apply")
    }
    if err := s.clearDefaultIPv4Route(); err != nil {
        s.l.Warn().Err(err).Msg("failed clearing default route prior to static apply")
    }

    ipNet := &net.IPNet{IP: ip, Mask: mask}
    addr := &netlink.Addr{IPNet: ipNet}
    if err := netlink.AddrAdd(iface, addr); err != nil {
        return logging.ErrorfL(s.l, "failed to add static IPv4 address", err)
    }
    s.l.Info().Str("ipv4", ipNet.String()).Msg("static IPv4 address applied")

    // Default route
    if gwStr != "" {
        gw := net.ParseIP(gwStr)
        if gw == nil || gw.To4() == nil {
            s.l.Warn().Str("gateway", gwStr).Msg("invalid IPv4 gateway; skipping route")
        } else {
            route := netlink.Route{LinkIndex: iface.Attrs().Index, Gw: gw}
            // remove any existing default routes already attempted above, then add
            if err := netlink.RouteAdd(&route); err != nil {
                // try replace if add failed
                if replaceErr := netlink.RouteReplace(&route); replaceErr != nil {
                    s.l.Warn().Err(err).Msg("failed to add default route")
                }
            }
            s.l.Info().Str("gateway", gwStr).Msg("default route applied")
        }
    }

    // DNS
    if len(dns) > 0 {
        if err := s.writeResolvConf(dns, s.GetDomain()); err != nil {
            s.l.Warn().Err(err).Msg("failed to write resolv.conf")
        }
    }

    // Refresh internal state
    if _, err := s.update(); err != nil {
        s.l.Warn().Err(err).Msg("failed to refresh state after static apply")
    }
    return nil
}
