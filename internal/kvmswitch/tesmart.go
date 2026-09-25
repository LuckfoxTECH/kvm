package kvmswitch

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"kvm/internal/logging"
)

var logger = logging.GetSubsystemLogger("kvmswitch")

type TESmartSwitch struct {
	mu           sync.Mutex
	ip           string
	port         int
	activePort   int
	pollInterval time.Duration
	cancelPoller context.CancelFunc
	enabled      bool
	lastCmdTime  time.Time
}

var (
	cmdHeader   = []byte{0xAA, 0xBB, 0x03}
	cmdFooter   = byte(0xEE)
	cmdSwitch   = byte(0x01)
	cmdQuery    = byte(0x10)
)

var (
	GlobalSwitch *TESmartSwitch
)

func NewTESmartSwitch() *TESmartSwitch {
	return &TESmartSwitch{
		activePort: -1,
	}
}

func (s *TESmartSwitch) UpdateConfig(enabled bool, ip string, port int, pollIntervalSec int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enabled = enabled
	s.ip = ip
	s.port = port
	s.pollInterval = time.Duration(pollIntervalSec) * time.Second

	if s.cancelPoller != nil {
		s.cancelPoller()
		s.cancelPoller = nil
	}

	if s.enabled && s.ip != "" {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelPoller = cancel
		go s.pollLoop(ctx)
	}
}

func (s *TESmartSwitch) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	// Initial poll
	s.QueryActivePort()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.QueryActivePort()
		}
	}
}

func (s *TESmartSwitch) executeCommand(cmd []byte, expectResponse bool) ([]byte, error) {
	elapsed := time.Since(s.lastCmdTime)
	if elapsed < 500*time.Millisecond {
		time.Sleep((500 * time.Millisecond) - elapsed)
	}
	s.lastCmdTime = time.Now()

	address := fmt.Sprintf("%s:%d", s.ip, s.port)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect error: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	_, err = conn.Write(cmd)
	if err != nil {
		return nil, fmt.Errorf("write error: %w", err)
	}

	if !expectResponse {
		return nil, nil
	}

	resp := make([]byte, 6)
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}

	return resp, nil
}

func (s *TESmartSwitch) SwitchPort(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return fmt.Errorf("switch is disabled")
	}

	cmd := []byte{cmdHeader[0], cmdHeader[1], cmdHeader[2], cmdSwitch, byte(port), cmdFooter}
	resp, err := s.executeCommand(cmd, true)
	if err != nil {
		return err
	}
	
	if len(resp) >= 6 && resp[0] == 0xAA && resp[1] == 0xBB {
		s.activePort = int(resp[4]) + 1
	} else {
		s.activePort = port
	}
	return nil
}

func (s *TESmartSwitch) QueryActivePort() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return -1, fmt.Errorf("switch is disabled")
	}

	cmd := []byte{cmdHeader[0], cmdHeader[1], cmdHeader[2], cmdQuery, 0x00, cmdFooter}
	resp, err := s.executeCommand(cmd, true)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to query active port")
		return -1, err
	}

	if len(resp) >= 6 && resp[0] == 0xAA && resp[1] == 0xBB {
		port := int(resp[4]) + 1
		s.activePort = port
		return port, nil
	}
	
	return -1, fmt.Errorf("invalid response length or header: %x", resp)
}

func (s *TESmartSwitch) GetActivePort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activePort
}

func InitSwitch() {
	GlobalSwitch = NewTESmartSwitch()
}

func TestConnection(ip string, port int) (int, time.Duration, error) {
	start := time.Now()
	address := fmt.Sprintf("%s:%d", ip, port)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return -1, 0, fmt.Errorf("connect error: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	cmd := []byte{cmdHeader[0], cmdHeader[1], cmdHeader[2], cmdQuery, 0x00, cmdFooter}
	_, err = conn.Write(cmd)
	if err != nil {
		return -1, 0, fmt.Errorf("write error: %w", err)
	}

	resp := make([]byte, 6)
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		return -1, 0, fmt.Errorf("read error: %w", err)
	}

	latency := time.Since(start)

	if len(resp) >= 6 && resp[0] == 0xAA && resp[1] == 0xBB {
		activePort := int(resp[4]) + 1
		return activePort, latency, nil
	}
	return -1, 0, fmt.Errorf("invalid response length or header: %x", resp)
}

func (s *TESmartSwitch) executeASCIICommand(cmd string, expectResponse bool) (string, error) {
	elapsed := time.Since(s.lastCmdTime)
	if elapsed < 500*time.Millisecond {
		time.Sleep((500 * time.Millisecond) - elapsed)
	}
	s.lastCmdTime = time.Now()

	address := fmt.Sprintf("%s:%d", s.ip, s.port)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("connect error: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	_, err = conn.Write([]byte(cmd))
	if err != nil {
		return "", fmt.Errorf("write error: %w", err)
	}

	if !expectResponse {
		return "", nil
	}

	resp := make([]byte, 256)
	n, err := conn.Read(resp)
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}

	return string(resp[:n]), nil
}

func (s *TESmartSwitch) QueryNetworkInfo() (string, string, string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return "", "", "", 0, fmt.Errorf("switch is disabled")
	}

	ipResp, err := s.executeASCIICommand("IP?\r\n", true)
	if err != nil {
		return "", "", "", 0, err
	}

	maResp, err := s.executeASCIICommand("MA?\r\n", true)
	if err != nil {
		return "", "", "", 0, err
	}

	gwResp, err := s.executeASCIICommand("GW?\r\n", true)
	if err != nil {
		return "", "", "", 0, err
	}
	
	ptResp, err := s.executeASCIICommand("PT?\r\n", true)
	if err != nil {
		return "", "", "", 0, err
	}

	parseValue := func(resp, prefix string) (string, error) {
		if len(resp) > 3 && resp[:3] == prefix {
			val := resp[3:]
			if len(val) > 0 && val[len(val)-1] == ';' {
				val = val[:len(val)-1]
			}
			return val, nil
		}
		return "", fmt.Errorf("invalid %s response: %s", prefix, resp)
	}

	ip, err := parseValue(ipResp, "IP:")
	if err != nil {
		return "", "", "", 0, err
	}
	mask, err := parseValue(maResp, "MA:")
	if err != nil {
		return "", "", "", 0, err
	}
	gw, err := parseValue(gwResp, "GW:")
	if err != nil {
		return "", "", "", 0, err
	}
	ptStr, err := parseValue(ptResp, "PT:")
	if err != nil {
		return "", "", "", 0, err
	}
	
	var pt int
	_, err = fmt.Sscanf(ptStr, "%d", &pt)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("invalid port value: %s", ptStr)
	}

	return ip, mask, gw, pt, nil
}

func (s *TESmartSwitch) ConfigureNetwork(newIP, newMask, newGateway string, newPort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return fmt.Errorf("switch is disabled")
	}

	_, err := s.executeASCIICommand(fmt.Sprintf("IP:%s;\r\n", newIP), false)
	if err != nil {
		return fmt.Errorf("failed to set IP: %w", err)
	}

	_, err = s.executeASCIICommand(fmt.Sprintf("MA:%s;\r\n", newMask), false)
	if err != nil {
		return fmt.Errorf("failed to set MA: %w", err)
	}

	_, err = s.executeASCIICommand(fmt.Sprintf("GW:%s;\r\n", newGateway), false)
	if err != nil {
		return fmt.Errorf("failed to set GW: %w", err)
	}

	// Port needs to be formatted with leading zeros to 5 digits, but "%05d" handles it.
	_, err = s.executeASCIICommand(fmt.Sprintf("PT:%05d;\r\n", newPort), false)
	if err != nil {
		return fmt.Errorf("failed to set PT: %w", err)
	}

	return nil
}
