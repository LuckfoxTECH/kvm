package kvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"kvm/internal/logging"
	"kvm/internal/usbgadget"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

type Session struct {
	peerConnection *webrtc.PeerConnection
	VideoTrack     *webrtc.TrackLocalStaticSample
	AudioTrack     *webrtc.TrackLocalStaticRTP
	//AudioTrack               *webrtc.TrackLocalStaticSample
	ControlChannel           *webrtc.DataChannel
	RPCChannel               *webrtc.DataChannel
	DiskChannel              *webrtc.DataChannel
	HidChannel               *webrtc.DataChannel
	shouldUmountVirtualMedia bool

	// hidRPCAvailable is set once the client completes the HID RPC handshake.
	hidRPCAvailable bool
	hidQueue        []chan hidQueueMessage

	// keysDownStateQueue carries device-truth keyboard state to the client.
	keysDownStateQueue chan usbgadget.KeysDownState

	// keep-alive jitter tracking (see handleHidRPCKeypressKeepAlive)
	keepAliveJitterLock      sync.Mutex
	lastKeepAliveArrivalTime time.Time
	lastTimerResetTime       time.Time
	keepAliveActive          bool

	macroLock sync.Mutex
	macroTask *hidMacroTask

	done      chan struct{}
	closeOnce sync.Once
}

// closeSignals releases the HID worker goroutines. Called on every terminal ICE
// state so a closed session does not leak one goroutine per HID queue.
func (s *Session) closeSignals() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.cancelHidMacro()
	})
}

type hidMacroTask struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Register at admission so cancellation also reaches a macro still in the queue.
func (s *Session) newHidMacro() *hidMacroTask {
	s.macroLock.Lock()
	defer s.macroLock.Unlock()
	if s.macroTask != nil {
		s.macroTask.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &hidMacroTask{ctx: ctx, cancel: cancel}
	s.macroTask = task
	if s.isClosed() {
		cancel()
	}
	return task
}

func (s *Session) cancelHidMacro() {
	s.macroLock.Lock()
	defer s.macroLock.Unlock()
	if s.macroTask != nil {
		s.macroTask.cancel()
	}
}

func (s *Session) finishHidMacro(task *hidMacroTask) {
	task.cancel()
	s.macroLock.Lock()
	defer s.macroLock.Unlock()
	if s.macroTask == task {
		s.macroTask = nil
	}
}

func (s *Session) isClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

const (
	hidQueueCount = 4
	hidQueueSize  = 256
)

// initHidQueues creates one ordered worker queue per HID traffic class (see
// hidrpc.GetQueueIndex) and starts a worker goroutine for each.
func (s *Session) initHidQueues() {
	s.hidQueue = make([]chan hidQueueMessage, 0, hidQueueCount)
	for i := 0; i < hidQueueCount; i++ {
		s.hidQueue = append(s.hidQueue, make(chan hidQueueMessage, hidQueueSize))
	}
	for _, queue := range s.hidQueue {
		go s.handleHidQueue(queue)
	}

	s.keysDownStateQueue = make(chan usbgadget.KeysDownState, hidQueueSize)
	go s.handleKeysDownStateQueue()
}

// handleKeysDownStateQueue pushes keyboard state changes to the client. A single
// worker keeps ordering and drops (instead of blocking) when the client is slow.
func (s *Session) handleKeysDownStateQueue() {
	for {
		select {
		case <-s.done:
			return
		case state := <-s.keysDownStateQueue:
			s.reportHidRPCKeysDownState(state)
		}
	}
}

func (s *Session) enqueueKeysDownState(state usbgadget.KeysDownState) {
	if s == nil || s.isClosed() || s.keysDownStateQueue == nil {
		return
	}

	select {
	case s.keysDownStateQueue <- state:
	case <-s.done:
	default:
		// The client is not draining state updates; dropping the oldest keeps the
		// queue bounded without stalling the USB HID write path.
		select {
		case <-s.keysDownStateQueue:
		default:
		}
		select {
		case s.keysDownStateQueue <- state:
		default:
		}
	}
}

func (s *Session) handleHidQueue(queue <-chan hidQueueMessage) {
	for {
		select {
		case <-s.done:
			return
		case msg := <-queue:
			if s.isClosed() {
				return
			}
			onHidMessage(msg, s)
		}
	}
}

func (s *Session) enqueueHidMessage(queueIndex int, msg hidQueueMessage) bool {
	if s == nil || s.isClosed() {
		return false
	}
	if queueIndex < 0 || queueIndex >= len(s.hidQueue) {
		return false
	}

	queue := s.hidQueue[queueIndex]
	if queue == nil {
		return false
	}

	select {
	case queue <- msg:
		return true
	case <-s.done:
		return false
	}
}

type SessionConfig struct {
	ICEServers []string
	LocalIP    string
	ws         *websocket.Conn
	Logger     *zerolog.Logger
}

const DefaultSTUN = "stun:stun.l.google.com:19302"

func buildICEServers() []webrtc.ICEServer {
	if config == nil {
		LoadConfig()
	}

	stunURL := config.STUN
	if stunURL == "" {
		stunURL = DefaultSTUN
	}

	servers := []webrtc.ICEServer{{URLs: []string{stunURL}}}
	for _, turnServer := range config.TurnServers {
		if turnServer.URL == "" {
			continue
		}
		servers = append(servers, webrtc.ICEServer{
			URLs:       []string{turnServer.URL},
			Username:   turnServer.Username,
			Credential: turnServer.Credential,
		})
	}

	return servers
}

func (s *Session) ExchangeOffer(offerStr string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(offerStr)
	if err != nil {
		return "", err
	}
	offer := webrtc.SessionDescription{}
	err = json.Unmarshal(b, &offer)
	if err != nil {
		return "", err
	}
	// Set the remote SessionDescription
	if err = s.peerConnection.SetRemoteDescription(offer); err != nil {
		return "", err
	}

	// Create answer
	answer, err := s.peerConnection.CreateAnswer(nil)
	if err != nil {
		return "", err
	}

	// Sets the LocalDescription, and starts our UDP listeners
	if err = s.peerConnection.SetLocalDescription(answer); err != nil {
		return "", err
	}

	localDescription, err := json.Marshal(s.peerConnection.LocalDescription())
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(localDescription), nil
}

func newSession(sessionConfig SessionConfig) (*Session, error) {
	webrtcSettingEngine := webrtc.SettingEngine{
		LoggerFactory: logging.GetPionDefaultLoggerFactory(),
	}
	webrtcSettingEngine.SetNetworkTypes([]webrtc.NetworkType{
		webrtc.NetworkTypeUDP4,
		webrtc.NetworkTypeUDP6,
	})
	//iceServer := webrtc.ICEServer{}

	var scopedLogger *zerolog.Logger
	if sessionConfig.Logger != nil {
		l := sessionConfig.Logger.With().Str("component", "webrtc").Logger()
		scopedLogger = &l
	} else {
		scopedLogger = webrtcLogger
	}

	iceServers := buildICEServers()

	api := webrtc.NewAPI(webrtc.WithSettingEngine(webrtcSettingEngine))
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		return nil, err
	}
	session := &Session{
		peerConnection: peerConnection,
		done:           make(chan struct{}),
	}
	session.initHidQueues()

	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		scopedLogger.Info().Str("label", d.Label()).Uint16("id", *d.ID()).Msg("New DataChannel")
		switch d.Label() {
		case "hidrpc":
			session.HidChannel = d
			d.OnMessage(getOnHidMessageHandler(session, scopedLogger, "hidrpc"))
			d.OnClose(session.cancelHidMacro)
		// the two unreliable channels are inbound only; nothing is sent on them
		case "hidrpc-unreliable-ordered":
			d.OnMessage(getOnHidMessageHandler(session, scopedLogger, "hidrpc-unreliable-ordered"))
		case "hidrpc-unreliable-nonordered":
			d.OnMessage(getOnHidMessageHandler(session, scopedLogger, "hidrpc-unreliable-nonordered"))
		case "rpc":
			session.RPCChannel = d
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				go onRPCMessage(msg, session)
			})
			triggerOTAStateUpdate()
			triggerVideoStateUpdate()
			triggerUSBStateUpdate()
		case "disk":
			session.DiskChannel = d
			d.OnMessage(onDiskMessage)
		case "terminal":
			handleTerminalChannel(d)
		case "serial":
			handleSerialChannel(d)
		default:
			if strings.HasPrefix(d.Label(), uploadIdPrefix) {
				go handleUploadChannel(d)
			}
		}
	})

	if streamEncodecType == "hevc" {
		session.VideoTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH265}, "video", "kvm")
	} else {
		session.VideoTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "kvm")
	}
	if err != nil {
		return nil, err
	}

	session.AudioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "kvm")
	if err != nil {
		return nil, err
	}

	rtpSender, err := peerConnection.AddTrack(session.VideoTrack)
	if err != nil {
		return nil, err
	}

	audioRtpSender, err := peerConnection.AddTrack(session.AudioTrack)
	if err != nil {
		return nil, err
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	go func() {
		audioRtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := audioRtpSender.Read(audioRtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	var isConnected bool

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		scopedLogger.Info().Interface("candidate", candidate).Msg("WebRTC peerConnection has a new ICE candidate")
		if candidate != nil {
			err := wsjson.Write(context.Background(), sessionConfig.ws, gin.H{"type": "new-ice-candidate", "data": candidate.ToJSON()})
			if err != nil {
				scopedLogger.Warn().Err(err).Msg("failed to write new-ice-candidate to WebRTC signaling channel")
			}
		}
	})

	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		scopedLogger.Info().Str("connectionState", connectionState.String()).Msg("ICE Connection State has changed")
		if connectionState == webrtc.ICEConnectionStateConnected {
			if !isConnected {
				isConnected = true
				actionSessions++
				onActiveSessionsChanged()
				setNpuAppStatus()
				if actionSessions == 1 {
					onFirstSessionConnected()
				}
			}
		}
		//state changes on closing browser tab disconnected->failed, we need to manually close it
		if connectionState == webrtc.ICEConnectionStateFailed {
			scopedLogger.Debug().Msg("ICE Connection State is failed, closing peerConnection")
			session.closeSignals()
			_ = peerConnection.Close()
		}
		if connectionState == webrtc.ICEConnectionStateClosed {
			scopedLogger.Debug().Msg("ICE Connection State is closed, unmounting virtual media")
			session.closeSignals()
			if session == currentSession {
				currentSession = nil
			}
			if session.shouldUmountVirtualMedia {
				err := unmountImage(false)
				scopedLogger.Warn().Err(err).Msg("unmount image failed on connection close")
			}
			if isConnected {
				isConnected = false
				actionSessions--
				onActiveSessionsChanged()
				if actionSessions == 0 {
					onLastSessionDisconnected()
				}
			}
		}
	})
	return session, nil
}

var actionSessions = 0

func onActiveSessionsChanged() {
	requestDisplayUpdate(true)
}

func onFirstSessionConnected() {
	_ = writeCtrlAction("start_video")
	if config.AudioMode != "disabled" {
		StartNtpAudioServer(handleAudioClient)
	}
}

func onLastSessionDisconnected() {
	// keep the native stream running when HTTP /video/stream subscribers are still watching
	if videoBroadcaster.SubscriberCount() == 0 {
		_ = writeCtrlAction("stop_video")
	}
	StopNtpAudioServer()

	// Release keys still held when the last client goes away, and cancel pending
	// auto-release timers first so a racing timer cannot reintroduce stale state
	// after the clear.
	if gadget != nil {
		gadget.CancelAllAutoReleaseTimers()
		_ = gadget.KeyboardReport(0, make([]uint8, 6))
	}
}
