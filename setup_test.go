package mikrotik

import (
	"testing"
	"time"

	"github.com/coredns/caddy"
)

func TestParseConfig(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    device 10.0.0.1:8728 admin pass
    address-list4 v4list
    address-list6 v6list
    timeout 1h30m
    comment production
    mask4 24
    mask6 64
    forward 8.8.8.8:53
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
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Matcher == nil {
		t.Fatal("expected non-nil Matcher")
	}
	if m.routes[0].AddressList4 != "" {
		t.Errorf("AddressList4: want empty, got %q", m.routes[0].AddressList4)
	}
	if m.routes[0].AddressList6 != "" {
		t.Errorf("AddressList6: want empty, got %q", m.routes[0].AddressList6)
	}
	if m.routes[0].Mask4 != 24 {
		t.Errorf("Mask4: want 24, got %d", m.routes[0].Mask4)
	}
	if m.routes[0].Mask6 != 64 {
		t.Errorf("Mask6: want 64, got %d", m.routes[0].Mask6)
	}
	if m.routes[0].Forward != "8.8.8.8:53" {
		t.Errorf("Forward: want %q, got %q", "8.8.8.8:53", m.routes[0].Forward)
	}
	if m.listForward != "8.8.8.8:53" {
		t.Errorf("listForward: want %q, got %q", "8.8.8.8:53", m.listForward)
	}
}

func TestParseConfigMinimal(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
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

// TestParseConfigNoDevice — device is now optional; config without device
// but with domains-file should succeed.
func TestParseConfigNoDevice(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    forward 1.1.1.1:53
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.writers) != 0 {
		t.Fatalf("expected 0 writers, got %d", len(m.writers))
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Matcher == nil {
		t.Fatal("expected non-nil Matcher")
	}
	if m.routes[0].Forward != "1.1.1.1:53" {
		t.Errorf("Forward: want %q, got %q", "1.1.1.1:53", m.routes[0].Forward)
	}
	if m.listForward != "1.1.1.1:53" {
		t.Errorf("listForward: want %q, got %q", "1.1.1.1:53", m.listForward)
	}
}

func TestParseConfigDeviceRequiredBeforeDirectives(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    address-list4 test
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for address-list4 before device")
	}
	if err.Error() != "address-list4 requires a device block" {
		t.Errorf("error message: want %q, got %q", "address-list4 requires a device block", err.Error())
	}
}

func TestParseConfigMultipleDevices(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
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

func TestParseConfigDomainsFileMissing(t *testing.T) {
	corefile := `mikrotik {
    domains-file /nonexistent/path/domains.txt
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for missing domains-file")
	}
}

func TestParseConfigDomainsFileRequired(t *testing.T) {
	corefile := `mikrotik {
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for missing domains-file")
	}
	if err.Error() != "domains-file is required" {
		t.Errorf("error message: want %q, got %q", "domains-file is required", err.Error())
	}
}

func TestParseConfigMask4Invalid(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    mask4 33
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for invalid mask4")
	}
}

func TestParseConfigMask6Invalid(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    mask6 129
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for invalid mask6")
	}
}

func TestParseConfigReload(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    reload 30s
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Matcher == nil {
		t.Fatal("expected non-nil Matcher")
	}
}

func TestParseConfigDefaults(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Mask4 != 0 {
		t.Errorf("Mask4: want 0, got %d", m.routes[0].Mask4)
	}
	if m.routes[0].Mask6 != 0 {
		t.Errorf("Mask6: want 0, got %d", m.routes[0].Mask6)
	}
	if m.routes[0].Forward != "" {
		t.Errorf("Forward: want empty, got %q", m.routes[0].Forward)
	}
	if m.listForward != "" {
		t.Errorf("listForward: want empty, got %q", m.listForward)
	}
}

func TestParseConfigForwardAndDevice(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    forward 8.8.8.8:53
    device 10.0.0.1:8728 admin pass
    address-list4 test
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.listForward != "8.8.8.8:53" {
		t.Errorf("listForward: want %q, got %q", "8.8.8.8:53", m.listForward)
	}
	if len(m.writers) != 1 {
		t.Fatalf("expected 1 writer, got %d", len(m.writers))
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Forward != "8.8.8.8:53" {
		t.Errorf("Forward: want %q, got %q", "8.8.8.8:53", m.routes[0].Forward)
	}
}

func TestParseConfigDomainsFileBeforeDevice(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    device 10.0.0.1:8728 admin pass
    address-list4 test
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Matcher == nil {
		t.Fatal("expected non-nil Matcher")
	}
	if len(m.writers) != 1 {
		t.Fatalf("expected 1 writer, got %d", len(m.writers))
	}
}

func TestParseConfigDeviceBeforeDomainsFile(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    device 10.0.0.1:8728 admin pass
    domains-file ` + domainPath + `
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Matcher == nil {
		t.Fatal("expected non-nil Matcher")
	}
	if len(m.writers) != 1 {
		t.Fatalf("expected 1 writer, got %d", len(m.writers))
	}
}

func TestParseConfigForwardMissingArg(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    forward
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for forward missing args")
	}
}

func TestParseConfigMask4Zero(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    mask4 0
    mask6 0
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Mask4 != 0 {
		t.Errorf("Mask4: want 0, got %d", m.routes[0].Mask4)
	}
	if m.routes[0].Mask6 != 0 {
		t.Errorf("Mask6: want 0, got %d", m.routes[0].Mask6)
	}
}

func TestParseConfigMask4Max(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    mask4 32
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Mask4 != 32 {
		t.Errorf("Mask4: want 32, got %d", m.routes[0].Mask4)
	}
}

func TestParseConfigMask6Max(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    mask6 128
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}
	if m.routes[0].Mask6 != 128 {
		t.Errorf("Mask6: want 128, got %d", m.routes[0].Mask6)
	}
}

func TestParseConfigAddressList6BeforeDevice(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    address-list6 v6
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for address-list6 before device")
	}
	if err.Error() != "address-list6 requires a device block" {
		t.Errorf("error message: want %q, got %q", "address-list6 requires a device block", err.Error())
	}
}

func TestParseConfigTimeoutBeforeDevice(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    timeout 1h
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for timeout before device")
	}
	if err.Error() != "timeout requires a device block" {
		t.Errorf("error message: want %q, got %q", "timeout requires a device block", err.Error())
	}
}

func TestParseConfigCommentBeforeDevice(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    comment mytag
    device 10.0.0.1:8728 admin pass
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for comment before device")
	}
	if err.Error() != "comment requires a device block" {
		t.Errorf("error message: want %q, got %q", "comment requires a device block", err.Error())
	}
}

func TestParseConfigUnknownDirective(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	corefile := `mikrotik {
    domains-file ` + domainPath + `
    bogus value
}`
	c := caddy.NewTestController("dns", corefile)
	_, err := parseConfig(c)
	if err == nil {
		t.Fatal("expected error for unknown directive")
	}
}
