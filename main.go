package kvm

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/gwatts/rootcerts"
)

var appCtx context.Context

func Main() {
	SyncConfigSD(true)
	LoadConfig()
	cleanupStaleLocalPackageOnStartup()

	if config.APIKey == "" {
		key, err := generateAPIKey()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to generate API key")
		} else {
			config.APIKey = key
			if err := SaveConfig(); err != nil {
				logger.Warn().Err(err).Msg("failed to save API key to config")
			} else {
				logger.Info().Msg("generated new API key")
			}
		}
	}

	var cancel context.CancelFunc
	appCtx, cancel = context.WithCancel(context.Background())
	defer cancel()

	systemVersionLocal, appVersionLocal, err := GetLocalVersion()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get local version")
	}

	minRequiredSystemVersion := semver.MustParse("0.1.4")
	isNewEnoughSystem := systemVersionLocal != nil && !systemVersionLocal.LessThan(minRequiredSystemVersion)

	logger.Info().
		Interface("system_version", systemVersionLocal).
		Interface("app_version", appVersionLocal).
		Msg("starting KVM")

	go watchAdcKeysLongPressReset(appCtx)
	go runWatchdog()
	go confirmCurrentSystem() //A/B system
	if isNewEnoughSystem {
		go setForceHpd()
	}

	http.DefaultClient.Timeout = 1 * time.Minute

	err = rootcerts.UpdateDefaultTransport()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load Root CA certificates")
	}
	logger.Info().
		Int("ca_certs_loaded", len(rootcerts.Certs())).
		Msg("loaded Root CA certificates")

	// Initialize network
	if err := initNetwork(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize network")
		os.Exit(1)
	}

	if err := ApplyFirewallConfig(config.Firewall); err != nil {
		logger.Warn().Err(err).Msg("failed to apply firewall config")
	}

	// Initialize time sync
	initTimeSync()
	timeSync.Start()

	// Initialize mDNS
	if err := initMdns(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize mDNS")
		os.Exit(1)
	}
	//if mDNS != nil {
	//	_ = mDNS.SetListenOptions(config.NetworkConfig.GetMDNSMode())
	//	_ = mDNS.SetLocalNames([]string{
	//		networkState.GetHostname(),
	//		networkState.GetFQDN(),
	//	}, true)
	//}

	// Initialize native ctrl socket server
	StartVideoCtrlSocketServer()

	// Initialize native video socket server
	StartVideoDataSocketServer()

	// Set up callbacks for HTTP video stream subscribers
	// When first HTTP subscriber connects and there's no WebRTC session, start video
	videoBroadcaster.onFirstSubscribe = func() {
		if getActiveSessions() == 0 {
			logger.Info().Msg("First HTTP video subscriber connected, starting video stream")
			_ = writeCtrlAction("start_video")
		}
	}
	// When last HTTP subscriber disconnects and there's no WebRTC session, stop video
	videoBroadcaster.onLastUnsubscribe = func() {
		if getActiveSessions() == 0 {
			logger.Info().Msg("Last HTTP video subscriber disconnected, stopping video stream")
			_ = writeCtrlAction("stop_video")
		}
	}

	// Initialize native audio socket server
	StartAudioCtrlSocketServer()

	StartVpnCtrlSocketServer()

	StartDisplayCtrlSocketServer()

	initPrometheus()

	go func() {
		err = ExtractAndRunVideoBin()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to extract and run video bin")
			//TODO: prepare an error message screen buffer to show on kvm screen
		}

		err = ExtractAndRunDisplayBin()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to extract and run display bin")
			//TODO: prepare an error message screen buffer to show on kvm screen
		}

		err = ExtractAndRunAudioBin()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to extract and run audio bin")
			//TODO: prepare an error message screen buffer to show on kvm screen
		}

		err = ExtractAndRunVpnBin()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to extract and run vpn bin")
			//TODO: prepare an error message screen buffer to show on kvm screen
		}
	}()

	if isNewEnoughSystem {
		// initialize usb gadget
		initUsbGadget()

		if err := setInitialVirtualMediaState(); err != nil {
			logger.Warn().Err(err).Msg("failed to set initial virtual media state")
		}

		if err := initImagesFolder(); err != nil {
			logger.Warn().Err(err).Msg("failed to init images folder")
		}
		remountPersistedVirtualMediaState()
		initJiggler()

		initSystemInfo()
		initAutoMountImage()
	}
	// initialize GPIO
	initGPIO()

	// initialize display
	initDisplay()

	// Initialize VPN
	initVPN()

	go RunWebServer()

	// API and MCP services temporarily disabled for debugging
	go func() {
		StartAPIServer(8080)
	}()

	go func() {
		StartMCP(8081, false)
	}()

	go RunWebSecureServer()
	// Web secure server is started only if TLS mode is enabled
	if config.TLSMode != "" {
		startWebSecureServer()
	}

	initSerialPort()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	logger.Info().Msg("KVM Shutting Down")
	//if fuseServer != nil {
	//	err := setMassStorageImage(" ")
	//	if err != nil {
	//		logger.Infof("Failed to unmount mass storage image: %v", err)
	//	}
	//	err = fuseServer.Unmount()
	//	if err != nil {
	//		logger.Infof("Failed to unmount fuse: %v", err)
	//	}

	// os.Exit(0)
}
