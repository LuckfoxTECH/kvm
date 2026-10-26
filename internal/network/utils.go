package network

import (
	"encoding/json"
	"sort"
	"time"
)

func lifetimeToTime(lifetime int) *time.Time {
	if lifetime == 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(lifetime) * time.Second)
	return &t
}

// sortIPv6Addresses orders the addresses in place so the primary IPv6 address
// picked by the caller does not depend on the order netlink returns.
func sortIPv6Addresses(addrs []IPv6Address) {
	sort.SliceStable(addrs, func(i, j int) bool {
		return addrs[i].Address.String() < addrs[j].Address.String()
	})
}

// isSameIPv6AddressList reports whether both lists hold the same addresses.
// Order is ignored and lifetimes are not compared because they tick down on
// every poll.
func isSameIPv6AddressList(a, b []IPv6Address) bool {
	if len(a) != len(b) {
		return false
	}

	for _, addr := range a {
		found := false
		for _, other := range b {
			if addr.Address.Equal(other.Address) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// ipv6AddressStrings returns the plain addresses for logging.
func ipv6AddressStrings(addrs []IPv6Address) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.Address.String())
	}
	return out
}

func IsSame(a, b interface{}) bool {
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aJSON) == string(bJSON)
}
