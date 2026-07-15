package mikrotik

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/coredns/caddy"
	"github.com/miekg/dns"
	"github.com/BearMaxDD/coredns-mikrotik/geosite"
	"google.golang.org/protobuf/proto"
)

// writeGeositeFile creates a temporary geosite.dat file with the given entries.
// Each entry is a map of countryCode -> []domain{type, value}.
func writeGeositeFile(t *testing.T, entries map[string][]struct {
	Type  geosite.Domain_Type
	Value string
}) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")

	var list geosite.GeoSiteList
	for code, domains := range entries {
		site := &geosite.GeoSite{CountryCode: code}
		for _, d := range domains {
			dt := d.Type
			site.Domain = append(site.Domain, &geosite.Domain{
				Type:  dt,
				Value: d.Value,
			})
		}
		list.Entry = append(list.Entry, site)
	}

	data, err := proto.Marshal(&list)
	if err != nil {
		t.Fatalf("marshal geosite: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write geosite: %v", err)
	}
	return path
}

func TestGeoSiteMatcherMatch(t *testing.T) {
	path := writeGeositeFile(t, map[string][]struct {
		Type  geosite.Domain_Type
		Value string
	}{
		"CN": {
			{Type: geosite.Domain_Plain, Value: "example.com"},
			{Type: geosite.Domain_RootDomain, Value: ".cn"},
			{Type: geosite.Domain_Full, Value: "exact.test.com"},
		},
	})

	gm, err := NewGeoSiteMatcher(path, "CN")
	if err != nil {
		t.Fatalf("NewGeoSiteMatcher: %v", err)
	}
	defer gm.Close()

	tests := []struct {
		name   string
		domain string
		want   bool
	}{
		{"plain match", "example.com.", true},
		{"plain subdomain", "sub.example.com.", true},
		{"rootdomain match", "something.cn.", true},
		{"rootdomain subdomain", "a.b.cn.", true},
		{"full exact", "exact.test.com.", true},
		{"full subdomain", "sub.exact.test.com.", false},
		{"no match", "other.net.", false},
		{"no match wrong tld", "example.org.", false},
		{"case insensitive", "EXAMPLE.COM.", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gm.Match(tc.domain)
			if got != tc.want {
				t.Errorf("Match(%q) = %v; want %v", tc.domain, got, tc.want)
			}
		})
	}
}

func TestGeoSiteMatcherCodeNotFound(t *testing.T) {
	path := writeGeositeFile(t, map[string][]struct {
		Type  geosite.Domain_Type
		Value string
	}{
		"US": {
			{Type: geosite.Domain_Plain, Value: "example.com"},
		},
	})

	_, err := NewGeoSiteMatcher(path, "CN")
	if err == nil {
		t.Fatal("expected error for missing country code")
	}
}

func TestGeoSiteMatcherInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.dat")
	if err := os.WriteFile(path, []byte("not-protobuf"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := NewGeoSiteMatcher(path, "CN")
	if err == nil {
		t.Fatal("expected error for invalid geosite file")
	}
}

func TestParseConfigGeoSite(t *testing.T) {
	domainPath := writeDomainFile(t, "example.com\n")
	geositePath := writeGeositeFile(t, map[string][]struct {
		Type  geosite.Domain_Type
		Value string
	}{
		"CN": {
			{Type: geosite.Domain_Plain, Value: "cn.example.com"},
		},
	})

	corefile := `mikrotik {
    domains-file ` + domainPath + `
    device 10.0.0.1:8728 admin pass
    address-list4 default-ipv4
    mask4 24
    geosite-file ` + geositePath + `
    geosite CN address-list4 cn-ipv4 mask4 32
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(m.routes))
	}

	// First route: domains-file, uses global defaults.
	r0 := m.routes[0]
	if r0.AddressList4 != "default-ipv4" {
		t.Errorf("route0 AddressList4: want %q, got %q", "default-ipv4", r0.AddressList4)
	}
	if r0.Mask4 != 24 {
		t.Errorf("route0 Mask4: want 24, got %d", r0.Mask4)
	}

	// Second route: geosite CN with per-route overrides.
	r1 := m.routes[1]
	if r1.AddressList4 != "cn-ipv4" {
		t.Errorf("route1 AddressList4: want %q, got %q", "cn-ipv4", r1.AddressList4)
	}
	if r1.Mask4 != 32 {
		t.Errorf("route1 Mask4: want 32, got %d", r1.Mask4)
	}
	if r1.Matcher == nil {
		t.Fatal("route1 Matcher is nil")
	}
	if !r1.Matcher.Match("cn.example.com.") {
		t.Error("route1 should match cn.example.com")
	}
	if r1.Matcher.Match("other.com.") {
		t.Error("route1 should not match other.com")
	}
}

func TestServeDNSMultiRoutePriority(t *testing.T) {
	// Two routes: first matches "blocked.test", second matches "*.test".
	// Verifies the first matching route is used, even though the second also matches.
	ctx := context.Background()

	m := &Mikrotik{
		routes: []RouteRule{
			{
				Matcher: &testDomainMatcher{
					matchFunc: func(domain string) bool {
						return domain == "blocked.test."
					},
				},
				Forward:      "8.8.8.8:53",
				AddressList4: "blocked-list",
				Mask4:        32,
			},
			{
				Matcher: &testDomainMatcher{
					matchFunc: func(domain string) bool {
						return domain == "blocked.test." || domain == "other.test."
					},
				},
				Forward: "1.1.1.1:53",
			},
		},
		writers: []*deviceWriter{
			{
				cfg:   DeviceConfig{Address: "10.0.0.1:8728"},
				queue: make(chan writeItem, 10),
			},
		},
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			qname := r.Question[0].Name
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("10.0.0.1"),
			})
			return resp, nil
		},
	}

	// Query that matches the first route (specific).
	req := new(dns.Msg)
	req.SetQuestion("blocked.test.", dns.TypeA)
	w := &testResponseWriter{}
	rcode, err := m.ServeDNS(ctx, w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rcode != dns.RcodeSuccess {
		t.Fatalf("expected RcodeSuccess, got %d", rcode)
	}

	if w.Msg == nil {
		t.Fatal("expected response")
	}

	// Check queue: first route's address-list and mask should be used.
	var items []writeItem
	for {
		select {
		case item := <-m.writers[0].queue:
			items = append(items, item)
		default:
			goto checkFirst
		}
	}
checkFirst:
	if len(items) != 1 {
		t.Fatalf("expected 1 writeItem, got %d", len(items))
	}
	if items[0].mask != 32 {
		t.Errorf("expected mask 32, got %d", items[0].mask)
	}

	// Now test a query that only the second route matches.


	req2 := new(dns.Msg)
	req2.SetQuestion("other.test.", dns.TypeA)
	w2 := &testResponseWriter{}
	rcode2, err2 := m.ServeDNS(ctx, w2, req2)
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if rcode2 != dns.RcodeSuccess {
		t.Fatalf("expected RcodeSuccess, got %d", rcode2)
	}

	// Check which route matched: second route has empty AddressList4,
	// so enqueueAddresses should skip (list == ""), queue stays empty.
	select {
	case item := <-m.writers[0].queue:
		t.Fatalf("expected empty queue for second route (no address-list), got %+v", item)
	default:
	}
}

func TestEnqueueAddressesPerRoute(t *testing.T) {
	// Test that enqueueAddresses uses route-level address-list/mask,
	// not writer-level.
	m := &Mikrotik{
		writers: []*deviceWriter{
			{
				cfg:   DeviceConfig{Address: "10.0.0.1:8728", AddressList4: "writer-v4", AddressList6: "writer-v6"},
				queue: make(chan writeItem, 10),
			},
		},
	}

	resp := new(dns.Msg)
	resp.Answer = append(resp.Answer,
		&dns.A{
			Hdr: dns.RR_Header{Name: "test.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.ParseIP("10.0.0.5"),
		},
		&dns.AAAA{
			Hdr: dns.RR_Header{Name: "test.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
			AAAA: net.ParseIP("2001:db8::1"),
		},
	)

	route := RouteRule{
		AddressList4: "route-v4",
		Mask4:        24,
		AddressList6: "route-v6",
		Mask6:        48,
	}

	m.enqueueAddresses(resp, route)

	var items []writeItem
	for {
		select {
		case item := <-m.writers[0].queue:
			items = append(items, item)
		default:
			goto done
		}
	}
done:
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	slices.SortFunc(items, func(a, b writeItem) int {
		if a.address < b.address {
			return -1
		}
		return 1
	})

	if items[0].list != "route-v4" {
		t.Errorf("v4 list: want %q, got %q", "route-v4", items[0].list)
	}
	if items[0].mask != 24 {
		t.Errorf("v4 mask: want 24, got %d", items[0].mask)
	}
	if items[1].list != "route-v6" {
		t.Errorf("v6 list: want %q, got %q", "route-v6", items[1].list)
	}
	if items[1].mask != 48 {
		t.Errorf("v6 mask: want 48, got %d", items[1].mask)
	}
}

func TestParseConfigGeoSiteDefaultsFallback(t *testing.T) {
	// GeoSite route without explicit address-list/mask falls back to globals.
	geositePath := writeGeositeFile(t, map[string][]struct {
		Type  geosite.Domain_Type
		Value string
	}{
		"US": {
			{Type: geosite.Domain_Plain, Value: "us.example.com"},
		},
	})

	corefile := `mikrotik {
    device 10.0.0.1:8728 admin pass
    address-list4 fallback-v4
    address-list6 fallback-v6
    mask4 16
    mask6 64
    geosite-file ` + geositePath + `
    geosite US
}`
	c := caddy.NewTestController("dns", corefile)
	m, err := parseConfig(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(m.routes))
	}

	r := m.routes[0]
	if r.AddressList4 != "fallback-v4" {
		t.Errorf("AddressList4: want %q, got %q", "fallback-v4", r.AddressList4)
	}
	if r.AddressList6 != "fallback-v6" {
		t.Errorf("AddressList6: want %q, got %q", "fallback-v6", r.AddressList6)
	}
	if r.Mask4 != 16 {
		t.Errorf("Mask4: want 16, got %d", r.Mask4)
	}
	if r.Mask6 != 64 {
		t.Errorf("Mask6: want 64, got %d", r.Mask6)
	}
}
