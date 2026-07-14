package mikrotik

import (
	"sync"
	"testing"
	"time"
)

func TestDedupHit(t *testing.T) {
	dw1 := &deviceWriter{
		cfg:   DeviceConfig{Address: "10.0.0.1:8728"},
		dedup: sync.Map{},
	}
	dw2 := &deviceWriter{
		cfg:   DeviceConfig{Address: "10.0.0.2:8728"},
		dedup: sync.Map{},
	}

	dw1.markDeduped("192.168.1.1", "allowed")

	// Same device+list+address should hit.
	if !dw1.isDeduped("192.168.1.1", "allowed") {
		t.Error("expected dedup hit for same device+list+address")
	}

	// Different list should not hit.
	if dw1.isDeduped("192.168.1.1", "blocked") {
		t.Error("expected no dedup hit for different list")
	}

	// Different device should not hit.
	if dw2.isDeduped("192.168.1.1", "allowed") {
		t.Error("expected no dedup hit for different device")
	}
}

func TestDedupExpired(t *testing.T) {
	dw := &deviceWriter{
		cfg:   DeviceConfig{Address: "10.0.0.1:8728"},
		dedup: sync.Map{},
	}

	key := dw.dedupKey("192.168.1.1", "allowed")

	// Store an already-expired deadline.
	dw.dedup.Store(key, time.Now().Add(-time.Nanosecond))

	if dw.isDeduped("192.168.1.1", "allowed") {
		t.Error("expected dedup miss for expired entry")
	}
}

func TestDedupKeyCollision(t *testing.T) {
	dw := &deviceWriter{
		cfg:   DeviceConfig{Address: "10.0.0.1:8728"},
		dedup: sync.Map{},
	}
	dw2 := &deviceWriter{
		cfg:   DeviceConfig{Address: "10.0.0.2:8728"},
		dedup: sync.Map{},
	}

	dw.markDeduped("192.168.1.1", "allowed")

	// Same device+list+address hits.
	if !dw.isDeduped("192.168.1.1", "allowed") {
		t.Error("expected dedup hit for same device+list+address")
	}

	// Same device, different list misses.
	if dw.isDeduped("192.168.1.1", "blocked") {
		t.Error("expected dedup miss for different list")
	}

	// Different device, same list+address misses.
	if dw2.isDeduped("192.168.1.1", "allowed") {
		t.Error("expected dedup miss for different device")
	}
}
