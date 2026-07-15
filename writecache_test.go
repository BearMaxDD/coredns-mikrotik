package mikrotik

import (
	"sync"
	"testing"
	"time"
)

func TestWriteCacheHit(t *testing.T) {
	wc := newWriteCache(time.Hour)
	key := "device|list|10.0.0.0/24"

	if wc.Has(key) {
		t.Fatal("expected miss before Set")
	}
	wc.Set(key)
	if !wc.Has(key) {
		t.Fatal("expected hit after Set")
	}
}

func TestWriteCacheExpired(t *testing.T) {
	wc := &writeCache{
		entries: map[string]time.Time{"k": time.Now().Add(-time.Second)},
		ttl:     time.Hour,
	}
	if wc.Has("k") {
		t.Fatal("expected miss for expired entry")
	}
	if _, ok := wc.entries["k"]; ok {
		t.Fatal("expected expired entry to be deleted on Has miss")
	}
}

func TestWriteCacheKeyCollision(t *testing.T) {
	wc := newWriteCache(time.Hour)
	k1 := cacheKey("dev1", "list", "10.0.0.0/24")
	k2 := cacheKey("dev2", "list", "10.0.0.0/24")

	wc.Set(k1)
	if !wc.Has(k1) {
		t.Fatal("k1 should hit")
	}
	if wc.Has(k2) {
		t.Fatal("k2 should miss — different device")
	}
}

func TestWriteCacheConcurrent(t *testing.T) {
	wc := newWriteCache(time.Minute)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wc.Set("key")
			wc.Has("key")
		}()
	}
	wg.Wait()
}

func TestWriteCacheTTLDefault(t *testing.T) {
	wc := newWriteCache(0)
	if wc.ttl != time.Hour {
		t.Fatalf("default TTL = %v, want 1h", wc.ttl)
	}
}

func TestEffectiveWriteCacheTTL(t *testing.T) {
	tests := []struct {
		name    string
		cfgTTL  time.Duration
		timeout time.Duration
		want    time.Duration
	}{
		{"default long timeout", 0, 24 * time.Hour, time.Hour},
		{"default short timeout", 0, 10 * time.Minute, 5 * time.Minute},
		{"explicit under timeout", time.Hour, 24 * time.Hour, time.Hour},
		{"explicit capped by timeout", 2 * time.Hour, time.Hour, time.Hour},
		{"explicit small", 30 * time.Minute, 24 * time.Hour, 30 * time.Minute},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveWriteCacheTTL(tc.cfgTTL, tc.timeout)
			if got != tc.want {
				t.Errorf("effectiveWriteCacheTTL(%v,%v) = %v, want %v", tc.cfgTTL, tc.timeout, got, tc.want)
			}
		})
	}
}
