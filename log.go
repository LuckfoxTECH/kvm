package kvm

import (
	"kvm/internal/logging"

	"github.com/rs/zerolog"
)

func ErrorfL(l *zerolog.Logger, format string, err error, args ...interface{}) error {
	return logging.ErrorfL(l, format, err, args...)
}

var (
	logger          = logging.GetSubsystemLogger("kvm")
	networkLogger   = logging.GetSubsystemLogger("network")
	vpnLogger       = logging.GetSubsystemLogger("vpn")
	websocketLogger = logging.GetSubsystemLogger("websocket")
	webrtcLogger    = logging.GetSubsystemLogger("webrtc")
	videoLogger     = logging.GetSubsystemLogger("video")
	audioLogger     = logging.GetSubsystemLogger("audio")
	nbdLogger       = logging.GetSubsystemLogger("nbd")
	timesyncLogger  = logging.GetSubsystemLogger("timesync")
	jsonRpcLogger   = logging.GetSubsystemLogger("jsonrpc")
	watchdogLogger  = logging.GetSubsystemLogger("watchdog")
	websecureLogger = logging.GetSubsystemLogger("websecure")
	otaLogger       = logging.GetSubsystemLogger("ota")
	serialLogger    = logging.GetSubsystemLogger("serial")
	terminalLogger  = logging.GetSubsystemLogger("terminal")
	displayLogger   = logging.GetSubsystemLogger("display")
	wolLogger       = logging.GetSubsystemLogger("wol")
	usbLogger       = logging.GetSubsystemLogger("usb")
	hidRPCLogger    = logging.GetSubsystemLogger("hidrpc")
	// external components
	ginLogger = logging.GetSubsystemLogger("gin")
)
