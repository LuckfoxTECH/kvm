//go:build linux

package usbgadget

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// newTestKeyboard returns a gadget whose keyboard writes go to a full pipe,
// like a HID endpoint whose last report the host hasn't collected yet, and
// the read end of that pipe with the number of bytes already in it.
func newTestKeyboard(t *testing.T) (*UsbGadget, *os.File, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})

	// Writes of up to a page are atomic, so top up a byte at a time once
	// whole pages no longer fit.
	filled := 0
	for _, size := range []int{4096, 1} {
		chunk := make([]byte, size)
		for {
			if err := w.SetWriteDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			n, err := w.Write(chunk)
			filled += n
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	logger := zerolog.Nop()
	u := &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		keyboardHidFile:       w,
	}
	return u, r, filled
}

func TestKeyboardReportWaitsForHost(t *testing.T) {
	u, r, filled := newTestKeyboard(t)

	// The host collects the earlier reports only after two write timeouts.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		time.Sleep(2*hidWriteTimeout + hidWriteTimeout/2)
		io.ReadFull(r, make([]byte, filled))
	}()

	if err := u.KeyboardReport(0, []byte{0x04}); err != nil {
		t.Fatal(err)
	}
	<-drained

	if err := r.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 8)
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatalf("report was dropped: %v", err)
	}
	if want := []byte{0, 0, 0x04, 0, 0, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("wrote %x, want %x", got, want)
	}
}

func TestKeyboardReportGivesUpWhenHostStopsReading(t *testing.T) {
	u, _, _ := newTestKeyboard(t)

	start := time.Now()
	if err := u.KeyboardReport(0, []byte{0x04}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*keyboardWriteAttempts*hidWriteTimeout {
		t.Fatalf("gave up after %s", elapsed)
	}
	if u.keyboardHidFile == nil {
		t.Fatal("HID file was closed after a write timeout")
	}
}
