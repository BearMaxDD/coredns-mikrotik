package mikrotik

import (
	"testing"
	"time"

	"github.com/coredns/caddy"
)

func TestParseConfig(t *testing.T) {
	corefile := `mikrotik {
    device 10.0.0.1:8728 admin pass
    address-list4 v4list
    address-list6 v6list
    timeout 1h30m
    comment production
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil Mikrotik")
	}
	if len(m.writers) != 1 {
		t.Fatalf("expected 1 writer, got %d", len(m.writers))
	}
	w := m.writers[0]
	if w.cfg.Address != "10.0.0.1:8728" {
		t.Errorf("Address: want %q, got %q", "10.0.0.1:8728", w.cfg.Address)
	}
	if w.cfg.Username != "admin" {
		t.Errorf("Username: want %q, got %q", "admin", w.cfg.Username)
	}
	if w.cfg.Password != "pass" {
		t.Errorf("Password: want %q, got %q", "pass", w.cfg.Password)
	}
	if w.cfg.AddressList4 != "v4list" {
		t.Errorf("AddressList4: want %q, got %q", "v4list", w.cfg.AddressList4)
	}
	if w.cfg.AddressList6 != "v6list" {
		t.Errorf("AddressList6: want %q, got %q", "v6list", w.cfg.AddressList6)
	}
	if w.cfg.Timeout != 90*time.Minute {
		t.Errorf("Timeout: want %v, got %v", 90*time.Minute, w.cfg.Timeout)
	}
	if w.cfg.Comment != "production" {
		t.Errorf("Comment: want %q, got %q", "production", w.cfg.Comment)
	}
}

func TestParseConfigMinimal(t *testing.T) {
	corefile := `mikrotik {
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.writers) != 1 {
		t.Fatalf("expected 1 writer, got %d", len(m.writers))
	}
	w := m.writers[0]
	if w.cfg.Timeout != 24*time.Hour {
		t.Errorf("Timeout: want default %v, got %v", 24*time.Hour, w.cfg.Timeout)
	}
	if w.cfg.AddressList4 != "" {
		t.Errorf("AddressList4: want empty, got %q", w.cfg.AddressList4)
	}
	if w.cfg.AddressList6 != "" {
		t.Errorf("AddressList6: want empty, got %q", w.cfg.AddressList6)
	}
	if w.cfg.Comment != "" {
		t.Errorf("Comment: want empty, got %q", w.cfg.Comment)
	}
}

func TestParseConfigNoDevice(t *testing.T) {
	corefile := `mikrotik {
    address-list4 test
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for missing device")
	}
	if err.Error() != "mikrotik: no device configured" {
		t.Errorf("error message: want %q, got %q", "mikrotik: no device configured", err.Error())
	}
}

func TestParseConfigDeviceRequiredBeforeDirectives(t *testing.T) {
	corefile := `mikrotik {
    address-list4 test
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for directive before device")
	}
}

func TestParseConfigMultipleDevices(t *testing.T) {
	corefile := `mikrotik {
    device 10.0.0.1:8728 admin pass
    address-list4 v4
    device 192.168.88.1:8728 read write
    address-list6 v6
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.writers) != 2 {
		t.Fatalf("expected 2 writers, got %d", len(m.writers))
	}
	w0 := m.writers[0]
	if w0.cfg.Address != "10.0.0.1:8728" {
		t.Errorf("writer0 Address: want %q, got %q", "10.0.0.1:8728", w0.cfg.Address)
	}
	if w0.cfg.AddressList4 != "v4" {
		t.Errorf("writer0 AddressList4: want %q, got %q", "v4", w0.cfg.AddressList4)
	}
	w1 := m.writers[1]
	if w1.cfg.Address != "192.168.88.1:8728" {
		t.Errorf("writer1 Address: want %q, got %q", "192.168.88.1:8728", w1.cfg.Address)
	}
	if w1.cfg.AddressList6 != "v6" {
		t.Errorf("writer1 AddressList6: want %q, got %q", "v6", w1.cfg.AddressList6)
	}
}
