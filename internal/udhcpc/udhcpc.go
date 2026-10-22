package udhcpc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	DHCPLeaseFile = "/run/udhcpc.%s.info"
	DHCPPidFile   = "/run/udhcpc.%s.pid"
)

type DHCPClient struct {
	InterfaceName  string
	leaseFile      string
	pidFile        string
	requestAddress string
	lease          *Lease
	logger         *zerolog.Logger
	process        *os.Process
	onLeaseChange  func(lease *Lease)
	enabled        bool
}

type DHCPClientOptions struct {
	InterfaceName  string
	PidFile        string
	Logger         *zerolog.Logger
	OnLeaseChange  func(lease *Lease)
	RequestAddress string
}

var defaultLogger = zerolog.New(os.Stdout).Level(zerolog.InfoLevel)

func NewDHCPClient(options *DHCPClientOptions) *DHCPClient {
	if options.Logger == nil {
		options.Logger = &defaultLogger
	}

	l := options.Logger.With().Str("interface", options.InterfaceName).Logger()
	return &DHCPClient{
		InterfaceName:  options.InterfaceName,
		logger:         &l,
		leaseFile:      fmt.Sprintf(DHCPLeaseFile, options.InterfaceName),
		pidFile:        options.PidFile,
		onLeaseChange:  options.OnLeaseChange,
		requestAddress: options.RequestAddress,
		enabled:        true,
	}
}

func (c *DHCPClient) getWatchPaths() []string {
	watchPaths := make(map[string]interface{})
	watchPaths[filepath.Dir(c.leaseFile)] = nil

	if c.pidFile != "" {
		watchPaths[filepath.Dir(c.pidFile)] = nil
	}

	paths := make([]string, 0)
	for path := range watchPaths {
		paths = append(paths, path)
	}
	return paths
}

func (c *DHCPClient) watchLink() {
	ch := make(chan netlink.LinkUpdate)
	done := make(chan struct{})

	if err := netlink.LinkSubscribe(ch, done); err != nil {
		c.logger.Error().Err(err).Msg("failed to subscribe to netlink")
		return
	}

	for update := range ch {
		if update.Link.Attrs().Name == c.InterfaceName {
			if update.Flags&unix.IFF_RUNNING != 0 {
				if c.enabled {
					c.logger.Info().Msg("link is up, starting udhcpc")
					go c.runUDHCPC()
				} else {
					c.logger.Debug().Msg("link is up, DHCP disabled")
				}
			} else {
				c.logger.Info().Msg("link is down")
			}
		}
	}
}

type udhcpcOutput struct {
	mu     *sync.Mutex
	logger *zerolog.Event
}

func (w *udhcpcOutput) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logger.Msg(string(p))
	return len(p), nil
}

func (c *DHCPClient) runUDHCPC() {
	if !c.enabled {
		c.logger.Debug().Msg("DHCP disabled; skipping udhcpc start")
		return
	}
	cmd := exec.Command("udhcpc", "-i", c.InterfaceName, "-t", "1")
	if c.requestAddress != "" {
		ip := net.ParseIP(c.requestAddress)
		if ip != nil && ip.To4() != nil {
			cmd.Args = append(cmd.Args, "-r", c.requestAddress)
		}
	}

	udhcpcOutputLock := sync.Mutex{}
	udhcpcStdout := &udhcpcOutput{
		mu:     &udhcpcOutputLock,
		logger: c.logger.Debug().Str("pipe", "stdout"),
	}
	udhcpcStderr := &udhcpcOutput{
		mu:     &udhcpcOutputLock,
		logger: c.logger.Debug().Str("pipe", "stderr"),
	}

	cmd.Stdout = udhcpcStdout
	cmd.Stderr = udhcpcStderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	if err := cmd.Run(); err != nil {
		c.logger.Error().Err(err).Msg("failed to run udhcpc")
	}
}

// Restart kills the current udhcpc, drops the interface address and starts a fresh
// DHCP discover. A plain renew (SIGUSR1) keeps asking the previous server for the
// previous address, so it cannot move the device to a different subnet; and with
// "-t 1" a failed renew makes udhcpc exit, leaving the interface without a client.
func (c *DHCPClient) Restart() error {
	if !c.enabled {
		return fmt.Errorf("dhcp client is disabled")
	}

	if err := c.KillProcess(); err != nil {
		c.logger.Warn().Err(err).Msg("failed to stop udhcpc before restart")
	}

	// Let the kernel tear the process down before the address is flushed.
	time.Sleep(200 * time.Millisecond)

	if err := flushInterfaceAddress(c.InterfaceName); err != nil {
		c.logger.Warn().Err(err).Str("interface", c.InterfaceName).Msg("failed to flush interface address")
	}

	// A pinned request address would keep the client on the old subnet.
	c.requestAddress = ""

	go c.runUDHCPC()
	return nil
}

func flushInterfaceAddress(iface string) error {
	cmd := exec.Command("ip", "addr", "flush", "dev", iface)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip addr flush dev %s: %w (%s)", iface, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Run starts the DHCP client and watches the lease file for changes.
// this isn't a blocking call, and the lease file is reloaded when a change is detected.
func (c *DHCPClient) Run() error {
	err := c.loadLeaseFile()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	go c.runUDHCPC()
	go c.watchLink()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					continue
				}
				if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
					continue
				}

				if event.Name == c.leaseFile {
					c.logger.Debug().
						Str("event", event.Op.String()).
						Str("path", event.Name).
						Msg("udhcpc lease file updated, reloading lease")
					_ = c.loadLeaseFile()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				c.logger.Error().Err(err).Msg("error watching lease file")
			}
		}
	}()

	for _, path := range c.getWatchPaths() {
		err = watcher.Add(path)
		if err != nil {
			c.logger.Error().
				Err(err).
				Str("path", path).
				Msg("failed to watch directory")
			return err
		}
	}

	// TODO: update udhcpc pid file
	// we'll comment this out for now because the pid might change
	// process := c.GetProcess()
	// if process == nil {
	// 	c.logger.Error().Msg("udhcpc process not found")
	// }

	// block the goroutine until the lease file is updated
	<-make(chan struct{})

	return nil
}

func (c *DHCPClient) loadLeaseFile() error {
	file, err := os.ReadFile(c.leaseFile)
	if err != nil {
		return err
	}

	data := string(file)
	if data == "" {
		c.logger.Debug().Msg("udhcpc lease file is empty")
		return nil
	}

	lease := &Lease{}
	err = UnmarshalDHCPCLease(lease, string(file))
	if err != nil {
		return err
	}

	isFirstLoad := c.lease == nil

	// Skip processing if lease hasn't changed to avoid unnecessary wake-ups.
	if reflect.DeepEqual(c.lease, lease) {
		return nil
	}

	c.lease = lease

	if lease.IPAddress == nil {
		c.logger.Info().
			Interface("lease", lease).
			Str("data", string(file)).
			Msg("udhcpc lease cleared")
		return nil
	}

	msg := "udhcpc lease updated"
	if isFirstLoad {
		msg = "udhcpc lease loaded"
	}

	leaseExpiry, err := lease.SetLeaseExpiry()
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to get dhcp lease expiry")
	} else {
		expiresIn := time.Until(leaseExpiry)
		c.logger.Info().
			Interface("expiry", leaseExpiry).
			Str("expiresIn", expiresIn.String()).
			Msg("current dhcp lease expiry time calculated")
	}

	c.onLeaseChange(lease)

	c.logger.Info().
		Str("ip", lease.IPAddress.String()).
		Str("leaseTime", lease.LeaseTime.String()).
		Interface("data", lease).
		Msg(msg)

	return nil
}

func (c *DHCPClient) GetLease() *Lease {
	return c.lease
}

// RequestAddress updates the requested IPv4 address and restarts udhcpc with -r <ip>.
func (c *DHCPClient) RequestAddress(ip string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("invalid IPv4 address: %s", ip)
	}
	c.requestAddress = ip
	_ = c.KillProcess()
	go c.runUDHCPC()
	return nil
}

// SetEnabled toggles DHCP client behavior. When enabling, it will attempt to start udhcpc.
// When disabling, it kills any running udhcpc process.
func (c *DHCPClient) SetEnabled(enable bool) {
	if c.enabled == enable {
		return
	}
	c.enabled = enable
	if enable {
		go c.runUDHCPC()
	} else {
		_ = c.KillProcess()
	}
}
