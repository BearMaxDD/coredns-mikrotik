package mikrotik

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestProcessItemWithFakeClient verifies that processItem successfully writes
// an address-list entry to the fake RouterOS client.
func TestProcessItemWithFakeClient(t *testing.T) {
	fc := &fakeClient{}
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "10.0.0.1:8728", Timeout: time.Hour},
		queue:  make(chan writeItem, 10),
		dedup:  sync.Map{},
		client: fc, // Pre-set to avoid real dial.
	}

	dw.processItem(context.Background(), writeItem{address: "192.168.1.1", list: "allowed"})

	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.history) == 0 {
		t.Error("expected at least one RouterOS command to be sent")
	}
}

// TestProcessItemDeduped verifies that a deduped item is skipped without
// sending any RouterOS commands.
func TestProcessItemDeduped(t *testing.T) {
	fc := &fakeClient{}
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "10.0.0.1:8728"},
		queue:  make(chan writeItem, 10),
		dedup:  sync.Map{},
		client: fc,
	}

	dw.markDeduped("192.168.1.1", "allowed")
	dw.processItem(context.Background(), writeItem{address: "192.168.1.1", list: "allowed"})

	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.history) != 0 {
		t.Errorf("expected no commands sent for deduped item, got %d", len(fc.history))
	}
}

// TestWorkerStartStop verifies that run() blocks until stop is closed,
// then returns without panicking.
func TestWorkerStartStop(t *testing.T) {
	dw := &deviceWriter{
		cfg:   DeviceConfig{Address: "10.0.0.1:8728"},
		queue: make(chan writeItem, 10),
		dedup: sync.Map{},
		stop:  make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		dw.run()
		close(done)
	}()

	// Ensure run() blocks until stop is closed.
	select {
	case <-done:
		t.Fatal("run() returned before stop was closed")
	case <-time.After(50 * time.Millisecond):
	}

	close(dw.stop)

	select {
	case <-done:
		// OK — run() exited after stop was closed.
	case <-time.After(time.Second):
		t.Fatal("run() did not return after stop was closed")
	}

	// Verify started flag is reset.
	if dw.started.Load() {
		t.Error("expected started to be false after run() returned")
	}
}
