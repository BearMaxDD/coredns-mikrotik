package mikrotik

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/coredns/coredns/plugin"
	"github.com/miekg/dns"
)

// testResponseWriter implements dns.ResponseWriter for testing.
type testResponseWriter struct {
	dns.ResponseWriter
	Msg *dns.Msg
}

func (w *testResponseWriter) WriteMsg(m *dns.Msg) error {
	w.Msg = m
	return nil
}

func (w *testResponseWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}
}

func (w *testResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}
}

// testDomainMatcher implements RuleMatcher for testing.
type testDomainMatcher struct {
	matchFunc func(domain string) bool
}

func (m *testDomainMatcher) Match(domain string) bool {
	return m.matchFunc(domain)
}
func (m *testDomainMatcher) Close() {}

// TestServeDNSDomainMatchExtract — first route matches; mock exchange returns
// A and AAAA records; verify queue items include the correct mask.
func TestServeDNSDomainMatchExtract(t *testing.T) {
	ctx := context.Background()
	m := &Mikrotik{
		routes: []RouteRule{
			{
				Matcher: &testDomainMatcher{
					matchFunc: func(domain string) bool { return domain == "example.com." },
				},
				AddressList4: "allowed-v4",
				AddressList6: "allowed-v6",
				Mask4:        24,
				Mask6:        64,
			},
		},
		writers: []*deviceWriter{
			{
				cfg:   DeviceConfig{Address: "10.0.0.1:8728", AddressList4: "allowed-v4", AddressList6: "allowed-v6"},
				queue: make(chan writeItem, 10),
				dedup: sync.Map{},
			},
		},
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer,
				&dns.A{
					Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP("10.0.0.5"),
				},
				&dns.AAAA{
					Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
					AAAA: net.ParseIP("2001:db8::1"),
				},
			)
			return resp, nil
		},
	}

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)

	w := &testResponseWriter{}
	rcode, err := m.ServeDNS(ctx, w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rcode != dns.RcodeSuccess {
		t.Fatalf("expected RcodeSuccess, got %d", rcode)
	}

	// Drain queue.
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
		t.Fatalf("expected 2 writeItems (1x A + 1x AAAA), got %d: %+v", len(items), items)
	}

	var v4Item, v6Item writeItem
	for _, item := range items {
		switch item.address {
		case "10.0.0.5":
			v4Item = item
		case "2001:db8::1":
			v6Item = item
		}
	}

	if v4Item.address != "10.0.0.5" || v4Item.list != "allowed-v4" {
		t.Errorf("expected 10.0.0.5 -> allowed-v4, got %s -> %s", v4Item.address, v4Item.list)
	}
	if v4Item.mask != 24 {
		t.Errorf("expected v4 mask 24, got %d", v4Item.mask)
	}
	if v6Item.address != "2001:db8::1" || v6Item.list != "allowed-v6" {
		t.Errorf("expected 2001:db8::1 -> allowed-v6, got %s -> %s", v6Item.address, v6Item.list)
	}
	if v6Item.mask != 64 {
		t.Errorf("expected v6 mask 64, got %d", v6Item.mask)
	}

	// Verify response was written back.
	if w.Msg == nil {
		t.Fatal("expected response to be written back to the client")
	}
}

// TestServeDNSDomainMatchNoWriters — route matches but no writers configured;
// verify forward is called and response is returned but queue stays empty.
func TestServeDNSDomainMatchNoWriters(t *testing.T) {
	ctx := context.Background()
	var exchanged bool
	m := &Mikrotik{
		routes: []RouteRule{
			{
				Matcher: &testDomainMatcher{
					matchFunc: func(domain string) bool { return true },
				},
			},
		},
		// No writers.
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			exchanged = true
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("10.0.0.5"),
			})
			return resp, nil
		},
	}

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)

	w := &testResponseWriter{}
	rcode, err := m.ServeDNS(ctx, w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rcode != dns.RcodeSuccess {
		t.Fatalf("expected RcodeSuccess, got %d", rcode)
	}
	if !exchanged {
		t.Error("expected exchange to be called")
	}
	if w.Msg == nil {
		t.Fatal("expected response to be written back")
	}
}

// TestServeDNSNoDomainMatch — no routes configured; verify query passes through
// to Next without exchange.
func TestServeDNSNoDomainMatch(t *testing.T) {
	ctx := context.Background()
	var nextCalled bool
	m := &Mikrotik{
		// No routes — no matching.
		Next: plugin.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
			nextCalled = true
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: "other.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("10.0.0.99"),
			})
			return dns.RcodeSuccess, w.WriteMsg(resp)
		}),
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			t.Error("exchange should not be called when no routes configured")
			return nil, nil
		},
	}

	req := new(dns.Msg)
	req.SetQuestion("other.com.", dns.TypeA)

	w := &testResponseWriter{}
	rcode, err := m.ServeDNS(ctx, w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rcode != dns.RcodeSuccess {
		t.Fatalf("expected RcodeSuccess, got %d", rcode)
	}
	if !nextCalled {
		t.Error("expected Next to be called")
	}
	if w.Msg == nil || len(w.Msg.Answer) == 0 {
		t.Fatal("expected response from Next to be written back")
	}
	if w.Msg.Answer[0].Header().Name != "other.com." {
		t.Errorf("expected other.com. answer, got %s", w.Msg.Answer[0].Header().Name)
	}
}

// TestServeDNSDomainMatchForwardError — route matches but exchange returns
// an error; verify SERVFAIL with no enqueue.
func TestServeDNSDomainMatchForwardError(t *testing.T) {
	ctx := context.Background()
	m := &Mikrotik{
		routes: []RouteRule{
			{
				Matcher: &testDomainMatcher{
					matchFunc: func(domain string) bool { return true },
				},
			},
		},
		writers: []*deviceWriter{
			{
				cfg:   DeviceConfig{Address: "10.0.0.1:8728", AddressList4: "allowed-v4"},
				queue: make(chan writeItem, 10),
				dedup: sync.Map{},
			},
		},
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			return nil, dns.ErrRcode
		},
	}

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)

	w := &testResponseWriter{}
	rcode, err := m.ServeDNS(ctx, w, req)
	if err == nil {
		t.Fatal("expected an error from exchange")
	}
	if rcode != dns.RcodeServerFailure {
		t.Fatalf("expected RcodeServerFailure, got %d", rcode)
	}

	// Queue should be empty.
	select {
	case item := <-m.writers[0].queue:
		t.Fatalf("expected empty queue, got item: %+v", item)
	default:
	}
}

// TestServeDNSDomainMatchNonIP — route matches but response has only non-IP
// records (CNAME, TXT); verify queue stays empty.
func TestServeDNSDomainMatchNonIP(t *testing.T) {
	ctx := context.Background()
	m := &Mikrotik{
		routes: []RouteRule{
			{
				Matcher: &testDomainMatcher{
					matchFunc: func(domain string) bool { return true },
				},
			},
		},
		writers: []*deviceWriter{
			{
				cfg:   DeviceConfig{Address: "10.0.0.1:8728", AddressList4: "allowed-v4"},
				queue: make(chan writeItem, 10),
				dedup: sync.Map{},
			},
		},
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer,
				&dns.CNAME{
					Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60},
					Target: "target.example.com.",
				},
				&dns.TXT{
					Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 60},
					Txt: []string{"v=spf1 include:_spf.example.com"},
				},
			)
			return resp, nil
		},
	}

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)

	w := &testResponseWriter{}
	rcode, err := m.ServeDNS(ctx, w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rcode != dns.RcodeSuccess {
		t.Fatalf("expected RcodeSuccess, got %d", rcode)
	}

	// Queue should be empty (no A/AAAA in answer).
	select {
	case item := <-m.writers[0].queue:
		t.Fatalf("expected empty queue for non-IP records, got item: %+v", item)
	default:
	}

	if w.Msg == nil {
		t.Fatal("expected response to be written back even with non-IP records")
	}
}
