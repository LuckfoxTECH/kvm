package usbgadget

import (
	"fmt"
	"sync"
	"time"
)

// hidOpenDrainTimeout bounds how long a rebind waits for in-flight HID opens.
const hidOpenDrainTimeout = 3 * time.Second

// hidOpenTracker counts HID descriptor opens that are still in flight. An open
// can outlive the caller that started it (the timeout path returns while the
// goroutine keeps running), so a rebind has to wait for those opens to return
// and close their old-generation descriptor before the controller is reset.
// Each drain group shares one completion channel, so repeated timeouts do not
// leave waiter goroutines behind.
type hidOpenTracker struct {
	mu      sync.Mutex
	pending int
	drained chan struct{}
}

func (h *hidOpenTracker) begin() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == 0 {
		h.drained = make(chan struct{})
	}
	h.pending++
}

func (h *hidOpenTracker) end() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending--
	if h.pending == 0 && h.drained != nil {
		close(h.drained)
		h.drained = nil
	}
}

// wait blocks until no HID open is in flight. The caller must hold hidLifecycle
// exclusively, which prevents a new admission from being recorded while waiting.
func (h *hidOpenTracker) wait(timeout time.Duration) error {
	h.mu.Lock()
	pending, drained := h.pending, h.drained
	h.mu.Unlock()
	if pending == 0 || drained == nil {
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-drained:
		return nil
	case <-timer.C:
		return fmt.Errorf("USB rebind aborted: HID opens did not drain within %s", timeout)
	}
}
