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

// TestServeDNSEnqueue — downstream returns an A record; verify queue gets one item.
func TestServeDNSEnqueue(t *testing.T) {
	ctx := context.Background()
	m := &Mikrotik{
		writers: []*deviceWriter{
			{
				cfg:   DeviceConfig{Address: "10.0.0.1:8728", AddressList4: "allowed-v4", AddressList6: "allowed-v6"},
				queue: make(chan writeItem, 10),
				dedup: sync.Map{},
			},
		},
		Next: plugin.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("10.0.0.5"),
			})
			err := w.WriteMsg(resp)
			return dns.RcodeSuccess, err
		}),
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

	select {
	case item := <-m.writers[0].queue:
		if item.address != "10.0.0.5" {
			t.Errorf("expected address 10.0.0.5, got %s", item.address)
		}
		if item.list != "allowed-v4" {
			t.Errorf("expected list allowed-v4, got %s", item.list)
		}
	default:
		t.Fatal("expected a writeItem in the queue, but queue was empty")
	}

	// Verify only one item was enqueued.
	if len(m.writers[0].queue) != 0 {
		t.Fatal("expected exactly one item in the queue")
	}
}

// TestServeDNSNoResponse — downstream returns SERVFAIL; verify queue stays empty.
func TestServeDNSNoResponse(t *testing.T) {
	ctx := context.Background()
	m := &Mikrotik{
		writers: []*deviceWriter{
			{
				cfg:   DeviceConfig{Address: "10.0.0.1:8728", AddressList4: "allowed-v4"},
				queue: make(chan writeItem, 10),
				dedup: sync.Map{},
			},
		},
		Next: plugin.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
			return dns.RcodeServerFailure, nil
		}),
	}

	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)

	w := &testResponseWriter{}
	rcode, err := m.ServeDNS(ctx, w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rcode != dns.RcodeServerFailure {
		t.Fatalf("expected RcodeServerFailure, got %d", rcode)
	}

	select {
	case item := <-m.writers[0].queue:
		t.Fatalf("expected empty queue, got item: %+v", item)
	default:
		// Queue is empty — expected.
	}
}

// TestServeDNSMultipleAddresses — downstream returns multiple A records;
// verify queue has multiple items.
func TestServeDNSMultipleAddresses(t *testing.T) {
	ctx := context.Background()
	m := &Mikrotik{
		writers: []*deviceWriter{
			{
				cfg:   DeviceConfig{Address: "10.0.0.1:8728", AddressList4: "allowed-v4", AddressList6: "allowed-v6"},
				queue: make(chan writeItem, 10),
				dedup: sync.Map{},
			},
		},
		Next: plugin.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer,
				&dns.A{
					Hdr: dns.RR_Header{Name: "svc1.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP("10.0.0.1"),
				},
				&dns.AAAA{
					Hdr: dns.RR_Header{Name: "svc2.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
					AAAA: net.ParseIP("2001:db8::1"),
				},
				&dns.A{
					Hdr: dns.RR_Header{Name: "svc3.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP("10.0.0.2"),
				},
				&dns.TXT{
					Hdr: dns.RR_Header{Name: "txt.example.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 60},
					Txt: []string{"hello"},
				},
			)
			err := w.WriteMsg(resp)
			return dns.RcodeSuccess, err
		}),
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

	// Drain the queue and count IP-based items.
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

	if len(items) != 3 {
		t.Fatalf("expected 3 writeItems (2x A + 1x AAAA), got %d: %+v", len(items), items)
	}

	// Check specific addresses.
	addrs := make(map[string]string)
	for _, item := range items {
		addrs[item.address] = item.list
	}
	if addrs["10.0.0.1"] != "allowed-v4" {
		t.Errorf("expected 10.0.0.1 -> allowed-v4, got %s", addrs["10.0.0.1"])
	}
	if addrs["10.0.0.2"] != "allowed-v4" {
		t.Errorf("expected 10.0.0.2 -> allowed-v4, got %s", addrs["10.0.0.2"])
	}
	if addrs["2001:db8::1"] != "allowed-v6" {
		t.Errorf("expected 2001:db8::1 -> allowed-v6, got %s", addrs["2001:db8::1"])
	}
}
