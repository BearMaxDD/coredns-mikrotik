package mikrotik

import (
	"context"
	"testing"
	"time"

	"github.com/coredns/caddy"
)

// 解析测试：refresh-on-hit 不设 dryRun
func TestRefreshOnHitParser(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	input := "mikrotik {\n    domains-file " + domainPath + "\n    refresh-on-hit\n}"
	m, err := parseConfig(caddy.NewTestController("dns", input))
	if err != nil { t.Fatal(err) }
	if !m.refreshOnHit { t.Fatal("expected refreshOnHit=true") }
}

// refresh-on-hit：cache hit 仍执行 RouterOS 命令
func TestRefreshOnHitExecutesCommands(t *testing.T) {
	fc := &fakeClient{}
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "10.0.0.1:8728", RefreshOnHit: true},
		queue:  make(chan writeItem, 10),
		wcache: newWriteCache(time.Hour),
		client: fc,
	}
	// 预置 cache
	dw.wcache.Set(cacheKey("10.0.0.1:8728", "allowed", "10.0.0.5"))
	dw.processItem(context.Background(), writeItem{
		address: "10.0.0.5", list: "allowed", mask: 0, domain: "example.com",
	})
	fc.mu.Lock()
	cmds := len(fc.history)
	fc.mu.Unlock()
	if cmds == 0 {
		t.Fatal("expected RouterOS commands despite cache hit (refresh-on-hit)")
	}
	// cache 应已续期
	if !dw.wcache.Has(cacheKey("10.0.0.1:8728", "allowed", "10.0.0.5")) {
		t.Fatal("expected cache extended after refresh")
	}
}

// 无 refresh-on-hit：cache hit 0 命令
func TestRefreshOnHitDisabledSkipsCommands(t *testing.T) {
	fc := &fakeClient{}
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "10.0.0.1:8728", RefreshOnHit: false},
		queue:  make(chan writeItem, 10),
		wcache: newWriteCache(time.Hour),
		client: fc,
	}
	dw.wcache.Set(cacheKey("10.0.0.1:8728", "allowed", "10.0.0.5"))
	dw.processItem(context.Background(), writeItem{
		address: "10.0.0.5", list: "allowed", mask: 0, domain: "example.com",
	})
	fc.mu.Lock()
	cmds := len(fc.history)
	fc.mu.Unlock()
	if cmds != 0 {
		t.Fatalf("expected 0 commands, got %d", cmds)
	}
}

// refresh-on-hit 时连接失败 → backoff
func TestRefreshOnHitBackoffOnFail(t *testing.T) {
	dw := &deviceWriter{
		cfg:    DeviceConfig{Address: "127.0.0.1:1", RefreshOnHit: true}, // connection refused fast
		queue:  make(chan writeItem, 10),
		wcache: newWriteCache(time.Hour),
	}
	dw.wcache.Set(cacheKey("127.0.0.1:1", "allowed", "10.0.0.5"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dw.processItem(ctx, writeItem{
		address: "10.0.0.5", list: "allowed", mask: 0, domain: "example.com",
	})
	if dw.backoff == 0 {
		t.Fatal("expected backoff > 0 after refresh-on-hit dial failure")
	}
}
