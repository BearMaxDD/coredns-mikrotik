package mikrotik

import (
	"context"
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
		wcache: newWriteCache(0), // default TTL=1h
		client: fc,               // Pre-set to avoid real dial.
	}

	dw.processItem(context.Background(), writeItem{address: "192.168.1.1", list: "allowed", mask: 0, domain: "test.example.com"})

	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.history) == 0 {
		t.Error("expected at least one RouterOS command to be sent")
	}
}

// TestProcessItemCacheHit verifies that a cached item is skipped without
// sending any RouterOS commands.
func TestProcessItemCacheHit(t *testing.T) {
	fc := &fakeClient{}
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "10.0.0.1:8728"},
		queue:  make(chan writeItem, 10),
		wcache: newWriteCache(time.Hour),
		client: fc,
	}

	// Pre-set cache so the processItem call should be a cache hit.
	dw.wcache.Set(cacheKey("10.0.0.1:8728", "allowed", "192.168.1.1"))

	dw.processItem(context.Background(), writeItem{address: "192.168.1.1", list: "allowed", mask: 0})

	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.history) != 0 {
		t.Errorf("expected no commands sent for cached item, got %d", len(fc.history))
	}

}

// TestWorkerStartStop verifies that run() blocks until stop is closed,
// then returns without panicking.
func TestWorkerStartStop(t *testing.T) {
	dw := &deviceWriter{
		cfg:   DeviceConfig{Address: "10.0.0.1:8728"},
		queue: make(chan writeItem, 10),
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

	if dw.started.Load() {
		t.Error("expected started to be false after run() returned")
	}
}
