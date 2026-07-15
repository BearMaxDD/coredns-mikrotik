package mikrotik

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestNoWriteIntegration(t *testing.T) {
	fc := &fakeClient{}
	domainPath := writeDomainFile(t, "no-write-domain.com\n")
	dl, err := NewDomainList(domainPath, 0)
	if err != nil { t.Fatal(err) }

	m := &Mikrotik{
		writers: []*deviceWriter{{
			cfg:    DeviceConfig{Address: "10.0.0.1:8728", Timeout: time.Hour},
			queue:  make(chan writeItem, 10),
			wcache: newWriteCache(time.Hour),
			client: fc,
		}},
		routes: []RouteRule{{
			Matcher: dl, AddressList4: "",
		}},
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("10.0.0.1").To4(),
			})
			return resp, nil
		},
	}

	r := new(dns.Msg)
	r.SetQuestion("no-write-domain.com.", dns.TypeA)
	rcode, err := m.ServeDNS(context.Background(), &testResponseWriter{}, r)
	if err != nil { t.Fatal(err) }
	if rcode != dns.RcodeSuccess { t.Fatalf("rcode=%d", rcode) }

	select {
	case <-m.writers[0].queue:
		t.Fatal("no-write should not enqueue any item")
	default:
	}
}

func TestWriteIntegration(t *testing.T) {
	fc := &fakeClient{}
	domainPath := writeDomainFile(t, "write-domain.com\n")
	dl, err := NewDomainList(domainPath, 0)
	if err != nil { t.Fatal(err) }

	m := &Mikrotik{
		writers: []*deviceWriter{{
			cfg:    DeviceConfig{Address: "10.0.0.1:8728", Timeout: time.Hour},
			queue:  make(chan writeItem, 10),
			wcache: newWriteCache(time.Hour),
			client: fc,
		}},
		routes: []RouteRule{{
			Matcher: dl, AddressList4: "test-list",
		}},
		exchange: func(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
			resp := new(dns.Msg)
			resp.SetReply(r)
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("10.0.0.1").To4(),
			})
			return resp, nil
		},
	}

	r := new(dns.Msg)
	r.SetQuestion("write-domain.com.", dns.TypeA)
	rcode, err := m.ServeDNS(context.Background(), &testResponseWriter{}, r)
	if err != nil { t.Fatal(err) }
	if rcode != dns.RcodeSuccess { t.Fatalf("rcode=%d", rcode) }

	select {
	case <-m.writers[0].queue:
		// OK — item enqueued
	default:
		t.Fatal("write route should enqueue an item")
	}
}
