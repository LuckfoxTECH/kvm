package timesync

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"kvm/internal/network"

	"github.com/rs/zerolog"
)

const (
	timeSyncRetryStep     = 5 * time.Second
	timeSyncRetryMaxInt   = 1 * time.Minute
	timeSyncWaitNetChkInt = 100 * time.Millisecond
	timeSyncWaitNetUpInt  = 3 * time.Second
	timeSyncInterval      = 1 * time.Hour
	timeSyncTimeout       = 2 * time.Second
)

var (
	timeSyncRetryInterval = 0 * time.Second
)

type TimeSync struct {
	syncLock *sync.Mutex
	l        *zerolog.Logger

	ntpServers    []string
	httpUrls      []string
	networkConfig *network.NetworkConfig

	rtcDevicePath string
	rtcDevice     *os.File //nolint:unused
	rtcLock       *sync.Mutex

	syncSuccess bool

	preCheckFunc func() (bool, error)
}

type TimeSyncOptions struct {
	PreCheckFunc  func() (bool, error)
	Logger        *zerolog.Logger
	NetworkConfig *network.NetworkConfig
}

type SyncMode struct {
	Disabled          bool
	Ntp               bool
	Http              bool
	Ordering        []string
	NtpUseFallback  bool
	HttpUseFallback bool
}

func NewTimeSync(opts *TimeSyncOptions) *TimeSync {
	rtcDevice, err := getRtcDevicePath()
	if err != nil {
		opts.Logger.Error().Err(err).Msg("failed to get RTC device path")
	} else {
		opts.Logger.Info().Str("path", rtcDevice).Msg("RTC device found")
	}

	t := &TimeSync{
		syncLock:      &sync.Mutex{},
		l:             opts.Logger,
		rtcDevicePath: rtcDevice,
		rtcLock:       &sync.Mutex{},
		preCheckFunc:  opts.PreCheckFunc,
		ntpServers:    activeNTPServers(opts.NetworkConfig),
		httpUrls:      activeHTTPUrls(opts.NetworkConfig),
		networkConfig: opts.NetworkConfig,
	}

	if t.rtcDevicePath != "" {
		rtcTime, _ := t.readRtcTime()
		t.l.Info().Interface("rtc_time", rtcTime).Msg("read RTC time")
	}

	return t
}

func (t *TimeSync) SetNetworkConfig(cfg *network.NetworkConfig) {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()
	t.networkConfig = cfg
	t.ntpServers = activeNTPServers(cfg)
	t.httpUrls = activeHTTPUrls(cfg)
}

func activeNTPServers(cfg *network.NetworkConfig) []string {
	var out []string
	if cfg != nil {
		// nil means "never configured" -> use every built-in server.
		// An empty (non-nil) slice means the user explicitly deselected all of them.
		if cfg.TimeSyncNtpServers == nil {
			out = append(out, DefaultNTPServers()...)
		} else {
			out = append(out, cfg.TimeSyncNtpServers...)
		}
		for _, c := range cfg.TimeSyncNtpCustom {
			if c.Enabled && c.Server != "" {
				out = append(out, c.Server)
			}
		}
	}
	return out
}

func activeHTTPUrls(cfg *network.NetworkConfig) []string {
	var out []string
	if cfg != nil {
		// nil means "never configured" -> use every built-in URL.
		// An empty (non-nil) slice means the user explicitly deselected all of them.
		if cfg.TimeSyncHttpUrls == nil {
			out = append(out, DefaultHTTPUrls()...)
		} else {
			out = append(out, cfg.TimeSyncHttpUrls...)
		}
		for _, c := range cfg.TimeSyncHttpCustom {
			if c.Enabled && c.Server != "" {
				out = append(out, c.Server)
			}
		}
	}
	return out
}

func (t *TimeSync) getSyncMode() SyncMode {
	syncMode := SyncMode{
		NtpUseFallback:  true,
		HttpUseFallback: true,
	}
	var syncModeString string

	if t.networkConfig != nil {
		syncModeString = t.networkConfig.TimeSyncMode.String
		if t.networkConfig.TimeSyncDisableFallback.Bool {
			syncMode.NtpUseFallback = false
			syncMode.HttpUseFallback = false
		}
	}

	switch syncModeString {
	case "ntp_only":
		syncMode.Ntp = true
	case "http_only":
		syncMode.Http = true
	case "disabled":
		syncMode.Disabled = true
	default:
		syncMode.Ntp = true
		syncMode.Http = true
	}

	return syncMode
}

func (t *TimeSync) doTimeSync() {
	metricTimeSyncStatus.Set(0)
	for {
		if ok, err := t.preCheckFunc(); !ok {
			if err != nil {
				t.l.Error().Err(err).Msg("pre-check failed")
			}
			time.Sleep(timeSyncWaitNetChkInt)
			continue
		}

		t.l.Info().Msg("syncing system time")
		start := time.Now()
		err := t.Sync()
		if err != nil {
			t.l.Error().Str("error", err.Error()).Msg("failed to sync system time")

			// retry after a delay
			timeSyncRetryInterval += timeSyncRetryStep
			time.Sleep(timeSyncRetryInterval)
			// reset the retry interval if it exceeds the max interval
			if timeSyncRetryInterval > timeSyncRetryMaxInt {
				timeSyncRetryInterval = 0
			}

			continue
		}
		t.syncSuccess = true
		t.l.Info().Str("now", time.Now().Format(time.RFC3339)).
			Str("time_taken", time.Since(start).String()).
			Msg("time sync successful")

		metricTimeSyncStatus.Set(1)

		time.Sleep(timeSyncInterval) // after the first sync is done
	}
}

func (t *TimeSync) Sync() error {
	var (
		now    *time.Time
		offset *time.Duration
	)

	syncMode := t.getSyncMode()

	// disabled mode: keep the current system date, skip all network sync
	if syncMode.Disabled {
		t.syncSuccess = true
		return nil
	}

	metricTimeSyncCount.Inc()

	if syncMode.Ntp {
		now, offset = t.queryNetworkTime()
	}

	if syncMode.Http && now == nil {
		now = t.queryAllHttpTime()
	}

	if now == nil {
		return fmt.Errorf("failed to get time from any source")
	}

	if offset != nil {
		newNow := time.Now().Add(*offset)
		now = &newNow
	}

	err := t.setSystemTime(*now)
	if err != nil {
		return fmt.Errorf("failed to set system time: %w", err)
	}

	metricTimeSyncSuccessCount.Inc()

	return nil
}

func (t *TimeSync) IsSyncSuccess() bool {
	return t.syncSuccess
}

func (t *TimeSync) Start() {
	go t.doTimeSync()
}

func (t *TimeSync) setSystemTime(now time.Time) error {
	nowStr := now.Format("2006-01-02 15:04:05")
	output, err := exec.Command("date", "-s", nowStr).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run date -s: %w, %s", err, string(output))
	}

	if t.rtcDevicePath != "" {
		return t.setRtcTime(now)
	}

	return nil
}
