package kvm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"

	"kvm/internal/logging"

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
	shouldUmountVirtualMedia bool
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
	session := &Session{peerConnection: peerConnection}

	// NewPeerConnection has already started the RTCP interceptor goroutines and
	// their tickers, so every failure path from here on has to close it.
	sessionReady := false
	defer func() {
		if !sessionReady {
			_ = peerConnection.Close()
		}
	}()

	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		scopedLogger.Info().Str("label", d.Label()).Uint16("id", *d.ID()).Msg("New DataChannel")
		switch d.Label() {
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
		if candidate != nil && sessionConfig.ws != nil {
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
				activeSessions := incrActiveSessions()
				onActiveSessionsChanged()
				setNpuAppStatus()
				if activeSessions == 1 {
					onFirstSessionConnected()
				}
			}
		}
		//state changes on closing browser tab disconnected->failed, we need to manually close it
		if connectionState == webrtc.ICEConnectionStateFailed {
			scopedLogger.Debug().Msg("ICE Connection State is failed, closing peerConnection")
			_ = peerConnection.Close()
		}
		if connectionState == webrtc.ICEConnectionStateClosed {
			scopedLogger.Debug().Msg("ICE Connection State is closed, unmounting virtual media")
			if session == currentSession {
				// No key-up is coming from this browser. Guarded on
				// currentSession so a handover doesn't clear the new one's keys.
				_ = rpcKeyboardReport(0, keyboardClearStateKeys)
				currentSession = nil
			}
			if session.shouldUmountVirtualMedia {
				err := rpcUnmountImage()
				scopedLogger.Warn().Err(err).Msg("unmount image failed on connection close")
			}
			if isConnected {
				isConnected = false
				activeSessions := decrActiveSessions()
				onActiveSessionsChanged()
				if activeSessions == 0 {
					onLastSessionDisconnected()
				}
			}
		}
	})

	sessionReady = true
	return session, nil
}

var (
	actionSessions      = 0
	activeSessionsMutex sync.Mutex
)

// incrActiveSessions, decrActiveSessions and getActiveSessions guard the
// active session counter. It is mutated from pion's ICE state-change
// callbacks, which run on per-PeerConnection goroutines, and read from the
// HTTP video broadcaster callbacks, so a bare int is a data race.
func incrActiveSessions() int {
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()

	actionSessions++
	return actionSessions
}

func decrActiveSessions() int {
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()

	actionSessions--
	return actionSessions
}

func getActiveSessions() int {
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()

	return actionSessions
}

func onActiveSessionsChanged() {
	requestDisplayUpdate(true)
}

func onFirstSessionConnected() {
	_ = writeCtrlAction("start_video")
	if config.AudioMode != "disabled" {
		StartNtpAudioServer(handleAudioClient)
	}
}

// keyboardClearStateKeys is an all-keys-up report; the HID report has six slots.
var keyboardClearStateKeys = make([]uint8, 6)

func onLastSessionDisconnected() {
	// Safety net: nobody is left to send a key-up.
	_ = rpcKeyboardReport(0, keyboardClearStateKeys)
	_ = writeCtrlAction("stop_video")
	StopNtpAudioServer()
}
