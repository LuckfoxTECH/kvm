package kvm

import (
	"errors"
	"fmt"
	"io"
	"time"

	"kvm/internal/hidrpc"
	"kvm/internal/mcp"
	"kvm/internal/usbgadget"

	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

// hidQueueMessage is the envelope handed to the per-session HID worker queue.
type hidQueueMessage struct {
	webrtc.DataChannelMessage
	channel string
	macro   *hidMacroTask
}

func handleHidRPCMessage(message hidrpc.Message, session *Session, task *hidMacroTask) {
	var rpcErr error

	switch message.Type() {
	case hidrpc.TypeHandshake:
		if session.HidChannel == nil {
			return
		}
		clientVersion, err := message.Version()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to read handshake version")
			return
		}
		if clientVersion != hidrpc.Version {
			hidRPCLogger.Warn().
				Uint8("client", clientVersion).
				Uint8("server", hidrpc.Version).
				Msg("hidrpc protocol version mismatch, closing channel")
			if err := session.HidChannel.Close(); err != nil {
				hidRPCLogger.Warn().Err(err).Msg("failed to close hid channel")
			}
			return
		}
		reply, err := hidrpc.NewHandshakeMessage().Marshal()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to marshal handshake message")
			return
		}
		if err := session.HidChannel.Send(reply); err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to send handshake message")
			return
		}
		session.hidRPCAvailable = true
		hidRPCLogger.Info().Uint8("version", clientVersion).Msg("HID RPC handshake completed")
	case hidrpc.TypeKeypressReport, hidrpc.TypeKeyboardReport:
		rpcErr = handleHidRPCKeyboardInput(message, session)
	case hidrpc.TypeKeyboardMacroReport:
		keyboardMacroReport, err := message.KeyboardMacroReport()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to get keyboard macro report")
			return
		}
		if task != nil {
			defer session.finishHidMacro(task)
			rpcErr = rpcExecuteKeyboardMacro(session, task, keyboardMacroReport)
		}
	case hidrpc.TypeCancelKeyboardMacroReport:
		// Cancels are normally intercepted before enqueueing; this is the
		// fallback for a cancel that did reach the worker.
		session.cancelHidMacro()
	case hidrpc.TypeKeypressKeepAliveReport:
		rpcErr = handleHidRPCKeypressKeepAlive(session)
	case hidrpc.TypePointerReport:
		pointerReport, err := message.PointerReport()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to get pointer report")
			return
		}
		rpcErr = rpcAbsMouseReport(pointerReport.X, pointerReport.Y, pointerReport.Button)
	case hidrpc.TypeMouseReport:
		mouseReport, err := message.MouseReport()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to get mouse report")
			return
		}
		rpcErr = rpcRelMouseReport(mouseReport.DX, mouseReport.DY, mouseReport.Button)
	case hidrpc.TypePenReport:
		penReport, err := message.PenReport()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to get pen report")
			return
		}
		rpcErr = rpcPenReport(
			penReport.X, penReport.Y,
			penReport.IsTip(), penReport.IsBarrel(), penReport.IsInRange(),
			penReport.Pressure,
		)
	case hidrpc.TypeTouchReport:
		touchReport, err := message.TouchReport()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to get touch report")
			return
		}
		contacts := make([]usbgadget.TouchContact, 0, len(touchReport.Contacts))
		for _, contact := range touchReport.Contacts {
			contacts = append(contacts, usbgadget.TouchContact{
				ID:      contact.ID,
				X:       contact.X,
				Y:       contact.Y,
				Down:    contact.IsDown(),
				InRange: contact.IsInRange(),
			})
		}
		rpcErr = rpcTouchReport(contacts)
	default:
		hidRPCLogger.Warn().Uint8("type", uint8(message.Type())).Msg("unknown HID RPC message type")
	}

	// Benign HID errors are expected when a function is switched off: the device
	// file is gone by design, so there is nothing to report as a failure.
	if rpcErr != nil && !usbgadget.IsHIDBenignError(rpcErr) {
		hidRPCLogger.Warn().Err(rpcErr).Msg("failed to handle HID RPC message")
	}
}

func onHidMessage(msg hidQueueMessage, session *Session) {
	data := msg.Data

	scopedLogger := hidRPCLogger.With().
		Str("channel", msg.channel).
		Bytes("data", data).
		Logger()
	scopedLogger.Trace().Msg("HID RPC message received")

	if len(data) < 1 {
		scopedLogger.Warn().Int("length", len(data)).Msg("received empty data in HID RPC message handler")
		return
	}

	var message hidrpc.Message
	if err := hidrpc.Unmarshal(data, &message); err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to unmarshal HID RPC message")
		return
	}

	if scopedLogger.GetLevel() <= zerolog.DebugLevel {
		scopedLogger = scopedLogger.With().Str("descr", message.String()).Logger()
	}

	t := time.Now()
	handleHidRPCMessage(message, session, msg.macro)
	d := time.Since(t)

	if d > 200*time.Millisecond {
		scopedLogger.Warn().Dur("duration", d).Msg("HID RPC message slow")
	} else {
		scopedLogger.Debug().Dur("duration", d).Msg("HID RPC message handled")
	}
}

// getOnHidMessageHandler returns a DataChannel handler that routes messages into
// one of the per-session ordered worker queues instead of spawning a goroutine
// per message. Mouse traffic alone can reach 1000 messages per second, so a
// goroutine per message would dominate the device CPU.
func getOnHidMessageHandler(session *Session, l *zerolog.Logger, channel string) func(msg webrtc.DataChannelMessage) {
	return func(msg webrtc.DataChannelMessage) {
		if len(msg.Data) < 1 {
			l.Warn().Str("channel", channel).Msg("received empty HID RPC message")
			return
		}

		// Cancel before admitting the next message on this channel: the macro
		// worker is busy sleeping, so routing the cancel through the queue would
		// deliver it only after the macro it is meant to stop has already run.
		if hidrpc.MessageType(msg.Data[0]) == hidrpc.TypeCancelKeyboardMacroReport {
			if !session.isClosed() {
				session.cancelHidMacro()
			} else {
				l.Debug().Str("channel", channel).Msg("dropping keyboard macro cancel for closed session")
			}
			return
		}

		queueIndex := hidrpc.GetQueueIndex(hidrpc.MessageType(msg.Data[0]))
		if queueIndex >= len(session.hidQueue) || queueIndex < 0 {
			l.Warn().Int("queueIndex", queueIndex).Msg("HID RPC queue index out of range, falling back to queue 3")
			queueIndex = 3
		}

		var task *hidMacroTask
		if hidrpc.MessageType(msg.Data[0]) == hidrpc.TypeKeyboardMacroReport {
			var message hidrpc.Message
			if err := hidrpc.Unmarshal(msg.Data, &message); err != nil {
				return
			}
			if _, err := message.KeyboardMacroReport(); err != nil {
				l.Warn().Err(err).Msg("invalid keyboard macro")
				return
			}
			task = session.newHidMacro()
		}
		if ok := session.enqueueHidMessage(queueIndex, hidQueueMessage{
			DataChannelMessage: msg,
			channel:            channel,
			macro:              task,
		}); !ok {
			if task != nil {
				session.finishHidMacro(task)
			}
			l.Warn().Int("queueIndex", queueIndex).Msg("HID RPC queue unavailable, dropping message")
		}
	}
}

// Keep-alive timing model. The client re-sends a keep-alive every
// expectedRate while a key is held; each valid tick pushes the auto-release
// deadline out. Timing mirrors real typing: the OS auto-repeat delay is 250ms
// on Linux (https://man.archlinux.org/man/kbdrate.8.en) and 1s on Windows
// (HKEY_CURRENT_USER\Control Panel\Accessibility\Keyboard Response\AutoRepeatDelay).
const expectedRate = 50 * time.Millisecond       // expected keepalive interval
const maxLateness = 50 * time.Millisecond        // jitter budget we still accept
const baseExtension = expectedRate + maxLateness // 100ms extension on a perfect tick
const maxStaleness = 225 * time.Millisecond      // discard ancient packets outright

// handleHidRPCKeypressKeepAlive extends the key auto-release window while the
// client keeps reporting that the key is still held.
//
// A late tick is not treated as "still held": small jitter (within maxLateness)
// shrinks the extension so total hold time still matches client intent, and
// large lateness (retransmit stall, ordering delay) is rejected outright. Ticks
// arriving after maxStaleness are ignored entirely so a zombie keep-alive
// cannot revive a key that should already be released.
func handleHidRPCKeypressKeepAlive(session *Session) error {
	if session == nil {
		return nil
	}

	session.keepAliveJitterLock.Lock()
	defer session.keepAliveJitterLock.Unlock()

	now := time.Now()

	if !session.keepAliveActive {
		return nil
	}
	if !session.lastTimerResetTime.IsZero() && now.Sub(session.lastTimerResetTime) > maxStaleness {
		session.keepAliveActive = false
		return nil
	}

	validTick := true
	timerExtension := baseExtension

	if !session.lastKeepAliveArrivalTime.IsZero() {
		lateness := now.Sub(session.lastKeepAliveArrivalTime) - expectedRate

		if lateness > 0 {
			if lateness <= maxLateness {
				// Normal network jitter: shrink the extension so the total hold
				// time stays aligned with what the client actually intends.
				timerExtension -= lateness
			} else {
				// Retransmit stall or ordering delay: reject the tick and leave
				// the deadline alone so auto-release still fires on time.
				validTick = false
			}
		}
	}

	if !validTick {
		session.lastKeepAliveArrivalTime = now
		return nil
	}

	release, err := mcp.AcquireInput(mcp.OwnerWeb, mcp.DefaultInputHold)
	if err != nil {
		return err
	}
	defer release()

	session.lastKeepAliveArrivalTime = now
	session.lastTimerResetTime = now

	if gadget != nil {
		gadget.DelayAutoReleaseWithDuration(timerExtension)
	}

	return nil
}

func handleHidRPCKeyboardInput(message hidrpc.Message, session *Session) error {
	release, err := mcp.AcquireInput(mcp.OwnerWeb, mcp.DefaultInputHold)
	if err != nil {
		return err
	}
	defer release()

	switch message.Type() {
	case hidrpc.TypeKeypressReport:
		keypressReport, err := message.KeypressReport()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to get keypress report")
			return err
		}
		if err := rpcKeypressReport(keypressReport.Key, keypressReport.Press); err != nil {
			return err
		}
		if keypressReport.Press {
			session.keepAliveJitterLock.Lock()
			session.keepAliveActive = true
			session.lastKeepAliveArrivalTime = time.Time{}
			session.lastTimerResetTime = time.Now()
			session.keepAliveJitterLock.Unlock()
		}
		return nil
	case hidrpc.TypeKeyboardReport:
		keyboardReport, err := message.KeyboardReport()
		if err != nil {
			hidRPCLogger.Warn().Err(err).Msg("failed to get keyboard report")
			return err
		}
		err = rpcKeyboardReport(keyboardReport.Modifier, keyboardReport.Keys)
		if err == nil {
			session.keepAliveJitterLock.Lock()
			session.keepAliveActive = false
			session.keepAliveJitterLock.Unlock()
		}
		return err
	}

	return fmt.Errorf("unknown HID RPC message type: %d", message.Type())
}

func reportHidRPC(params any, session *Session) {
	if session == nil || session.isClosed() {
		hidRPCLogger.Debug().Msg("session is nil, skipping reportHidRPC")
		return
	}

	if !session.hidRPCAvailable || session.HidChannel == nil {
		// Expected during the WebRTC handshake, before the HID channel is ready.
		hidRPCLogger.Trace().
			Bool("hidRPCAvailable", session.hidRPCAvailable).
			Bool("HidChannel", session.HidChannel != nil).
			Msg("HID RPC is not available, skipping reportHidRPC")
		return
	}

	var (
		message []byte
		err     error
	)
	switch params := params.(type) {
	case usbgadget.KeyboardState:
		message, err = hidrpc.NewKeyboardLedMessage(params).Marshal()
	case usbgadget.KeysDownState:
		message, err = hidrpc.NewKeydownStateMessage(params).Marshal()
	case hidrpc.KeyboardMacroState:
		message, err = hidrpc.NewKeyboardMacroStateMessage(params.State, params.IsPaste).Marshal()
	default:
		err = fmt.Errorf("unknown HID RPC message type: %T", params)
	}

	if err != nil {
		hidRPCLogger.Warn().Err(err).Msg("failed to marshal HID RPC message")
		return
	}

	if message == nil {
		hidRPCLogger.Warn().Msg("failed to marshal HID RPC message")
		return
	}

	if err := session.HidChannel.Send(message); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			hidRPCLogger.Debug().Err(err).Msg("HID RPC channel closed, skipping reportHidRPC")
			return
		}
		hidRPCLogger.Warn().Err(err).Msg("failed to send HID RPC message")
	}
}

// reportHidRPCKeyboardLedState pushes the host LED state to the client. It falls
// back to the legacy JSON-RPC event while the client has no HID RPC channel.
func (s *Session) reportHidRPCKeyboardLedState(state usbgadget.KeyboardState) {
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keyboardLedState", state, s)
	}
	reportHidRPC(state, s)
}

// reportHidRPCKeysDownState pushes the modifier/pressed-key state to the client.
// This is what lets the frontend highlight the exact side of a modifier key.
func (s *Session) reportHidRPCKeysDownState(state usbgadget.KeysDownState) {
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keysDownState", state, s)
	}
	reportHidRPC(state, s)
}

// reportHidRPCKeyboardMacroState pushes keyboard macro progress to the client.
func (s *Session) reportHidRPCKeyboardMacroState(state hidrpc.KeyboardMacroState) {
	if !s.hidRPCAvailable {
		writeJSONRPCEvent("keyboardMacroState", state, s)
	}
	reportHidRPC(state, s)
}
