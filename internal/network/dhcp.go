package network

type DhcpTargetState int

const (
	DhcpTargetStateDoNothing DhcpTargetState = iota
	DhcpTargetStateStart
	DhcpTargetStateStop
	DhcpTargetStateRenew
	DhcpTargetStateRelease
	// DhcpTargetStateApplyStaticIPv4 reapplies the static IPv4 address, default
	// route and DNS. The kernel drops interface addresses on link transitions
	// (cable replug, MAC change), so the static config must be reapplied then.
	DhcpTargetStateApplyStaticIPv4
)
