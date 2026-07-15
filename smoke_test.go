package mikrotik

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestSmokeRouting(t *testing.T) {
	fc := &fakeClient{}
	pathA := writeDomainFile(t, "route-a.com\n")
	pathB := writeDomainFile(t, "route-b.com\n")
	dlA, _ := NewDomainList(pathA, 0)
	dlB, _ := NewDomainList(pathB, 0)

	m := &Mikrotik{
		writers: []*deviceWriter{{
			cfg:    DeviceConfig{Address: "10.0.0.1:8728"},
			queue:  make(chan writeItem, 10),
			wcache: newWriteCache(time.Hour),
			client: fc,
		}},
		routes: []RouteRule{
			{Matcher: dlA, AddressList4: "list-a"},
			{Matcher: dlB},
		},
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

	tests := []struct {
		name    string
		domain  string
		wantEnq bool
	}{
		{"route A write", "route-a.com", true},
		{"route B no-write", "route-b.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := new(dns.Msg)
			r.SetQuestion(tc.domain+".", dns.TypeA)
			w := &testResponseWriter{}
			rcode, err := m.ServeDNS(context.Background(), w, r)
			if err != nil { t.Fatal(err) }
			if rcode != dns.RcodeSuccess { t.Fatalf("rcode=%d", rcode) }

			if tc.wantEnq {
				select {
				case <-m.writers[0].queue:
				default:
					t.Fatal("expected enqueue")
				}
			} else {
				select {
				case <-m.writers[0].queue:
					t.Fatal("expected no enqueue")
				default:
				}
			}
		})
	}
}
