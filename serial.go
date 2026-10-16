package kvm

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
	"go.bug.st/serial"
)

const serialPortPath = "/dev/ttyS0"

// serialSubscriberQueue is how many 1 KiB chunks are queued per viewer.
const serialSubscriberQueue = 64

var defaultMode = &serial.Mode{
	BaudRate: 115200,
	DataBits: 8,
	Parity:   serial.NoParity,
	StopBits: serial.OneStopBit,
}

// serialPump owns the serial port. Exactly one goroutine reads /dev/ttyS0 and
// fans the bytes out to every viewer. The tty layer hands each byte to only one
// reader, so per-connection readers would steal bytes from each other and make
// every viewer see a partial stream. The port lifetime follows the user intent
// (connectSerial / disconnectSerial), never the viewers.
type serialPump struct {
	openMu    sync.Mutex
	mu        sync.Mutex
	port      serial.Port
	subs      map[int]chan []byte
	nextSubID int
}

var serialReader = &serialPump{subs: map[int]chan []byte{}}

// initSerialPort starts the single serial reader and reopens the port when the
// user left it connected, so the serial line survives reboots. A fresh install
// keeps the port closed until connectSerial is called.
func initSerialPort() {
	go serialReader.run()

	if serialPortEnabled {
		if err := reopenSerialPort(); err != nil {
			serialLogger.Error().Err(err).Msg("Failed to restore serial port connection")
		}
	}
}

// getSerialPort returns the current shared serial port, or nil when closed.
func getSerialPort() serial.Port {
	return serialReader.get()
}

// closeSerialPort closes and clears the shared serial port.
// Safe to call when the port is already closed.
func closeSerialPort() {
	serialReader.closePort()
}

func reopenSerialPort() error {
	serialReader.closePort()

	p, err := serial.Open(serialPortPath, serialPortMode)
	if err != nil {
		serialLogger.Error().
			Err(err).
			Str("path", serialPortPath).
			Interface("mode", serialPortMode).
			Msg("Error opening serial port")
		return err
	}

	serialReader.mu.Lock()
	serialReader.port = p
	serialReader.mu.Unlock()

	serialLogger.Info().
		Str("path", serialPortPath).
		Interface("mode", serialPortMode).
		Msg("Serial port opened successfully")
	return nil
}

// ensureSerialPortOpen opens the port only when it is closed. Reopening a port
// that is already open would drop whatever the kernel has already buffered.
func ensureSerialPortOpen() error {
	serialReader.openMu.Lock()
	defer serialReader.openMu.Unlock()

	if getSerialPort() != nil {
		return nil
	}
	return reopenSerialPort()
}

func (sp *serialPump) get() serial.Port {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.port
}

func (sp *serialPump) closePort() {
	sp.mu.Lock()
	p := sp.port
	sp.port = nil
	sp.mu.Unlock()

	if p != nil {
		_ = p.Close()
	}
}

// run is the only reader of the serial port for the whole process lifetime.
func (sp *serialPump) run() {
	buf := make([]byte, 1024)
	for {
		p := sp.get()
		if p == nil {
			// Port closed (disconnected or no viewer yet).
			time.Sleep(20 * time.Millisecond)
			continue
		}

		n, err := p.Read(buf)
		if err != nil {
			if err != io.EOF {
				serialLogger.Warn().Err(err).Msg("Failed to read from serial port")
			}
			// The handle may have been replaced by a reopen; wait a moment and
			// pick up the current one instead of exiting.
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if n == 0 {
			continue
		}

		sp.broadcast(buf[:n])
	}
}

// broadcast fans one chunk out to every viewer. It never blocks: a viewer that
// cannot keep up loses its oldest queued chunk instead of stalling the port.
func (sp *serialPump) broadcast(data []byte) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if len(sp.subs) == 0 {
		return
	}

	// buf is reused by the reader, so subscribers get their own copy.
	payload := make([]byte, len(data))
	copy(payload, data)

	for _, ch := range sp.subs {
		select {
		case ch <- payload:
		default:
			// Slow viewer: drop the oldest chunk, keep the newest bytes.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- payload:
			default:
			}
		}
	}
}

// subscribe registers a viewer and returns its id and data channel.
func (sp *serialPump) subscribe() (int, <-chan []byte) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	sp.nextSubID++
	id := sp.nextSubID
	ch := make(chan []byte, serialSubscriberQueue)
	sp.subs[id] = ch

	return id, ch
}

// unsubscribe removes a viewer. The port stays open: its lifetime follows the
// persisted user intent, so reloading the page must not drop the serial line.
func (sp *serialPump) unsubscribe(id int) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if ch, ok := sp.subs[id]; ok {
		delete(sp.subs, id)
		close(ch)
	}
}

// serialSubscription couples a pump subscriber with the viewer lifecycle, so a
// channel that closes early can never leak a subscriber.
type serialSubscription struct {
	mu     sync.Mutex
	id     int
	closed bool
}

func (s *serialSubscription) add() (<-chan []byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, false
	}

	id, ch := serialReader.subscribe()
	s.id = id
	return ch, true
}

func (s *serialSubscription) remove() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true

	if s.id != 0 {
		serialReader.unsubscribe(s.id)
		s.id = 0
	}
}

// isTerminalSizeMessage reports whether data is a JSON terminal-size payload,
// which must not be forwarded to the serial device.
func isTerminalSizeMessage(data []byte) bool {
	maybeJSON := bytes.TrimSpace(data)
	if len(maybeJSON) <= 1 || maybeJSON[0] != '{' || maybeJSON[len(maybeJSON)-1] != '}' {
		return false
	}
	var size TerminalSize
	return json.Unmarshal(maybeJSON, &size) == nil
}

func handleSerialChannel(d *webrtc.DataChannel) {
	scopedLogger := serialLogger.With().
		Uint16("data_channel_id", *d.ID()).Logger()

	sub := &serialSubscription{}

	d.OnOpen(func() {
		// The port is opened by an explicit Connect only; attaching a viewer must
		// not open the serial line behind the user's back.
		ch, ok := sub.add()
		if !ok {
			return
		}

		go func() {
			for data := range ch {
				if err := d.Send(data); err != nil {
					scopedLogger.Warn().Err(err).Msg("Failed to send serial output")
					sub.remove()
					return
				}
			}
		}()
	})

	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString && isTerminalSizeMessage(msg.Data) {
			return
		}
		p := getSerialPort()
		if p == nil {
			return
		}
		if _, err := p.Write(msg.Data); err != nil {
			scopedLogger.Warn().Err(err).Msg("Failed to write to serial")
		}
	})

	d.OnError(func(err error) {
		scopedLogger.Warn().Err(err).Msg("Serial channel error")
	})

	d.OnClose(func() {
		sub.remove()
		scopedLogger.Info().Msg("Serial channel closed")
	})
}

func handleSerialWS(c *gin.Context) {
	source := c.ClientIP()
	scopedLogger := serialLogger.With().
		Str("transport", "websocket").
		Str("source", source).
		Logger()

	wsCon, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		c.Status(500)
		return
	}
	defer wsCon.Close(websocket.StatusNormalClosure, "")

	scopedLogger.Info().Msg("Serial websocket connected")

	sub := &serialSubscription{}
	ch, ok := sub.add()
	if !ok {
		wsCon.Close(websocket.StatusInternalError, "")
		return
	}
	defer sub.remove()

	ctx := c.Request.Context()

	// serial -> websocket
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for data := range ch {
			if err := wsCon.Write(ctx, websocket.MessageBinary, data); err != nil {
				return
			}
		}
	}()

	// websocket -> serial
	for {
		msgType, data, readErr := wsCon.Read(ctx)
		if readErr != nil {
			break
		}

		if msgType == websocket.MessageText && isTerminalSizeMessage(data) {
			continue
		}

		p := getSerialPort()
		if p == nil {
			continue
		}
		if _, err := p.Write(data); err != nil {
			break
		}
	}

	// Detach first so the writer loop ends, then close the connection to unblock
	// a write that is already in flight.
	sub.remove()
	_ = wsCon.Close(websocket.StatusNormalClosure, "")
	<-writerDone
}
