package mikrotik

import (
	"context"
	"net"

	"github.com/coredns/coredns/plugin"
	"github.com/miekg/dns"
)

// Name implements plugin.Handler.
func (m *Mikrotik) Name() string { return "mikrotik" }

// ServeDNS intercepts DNS queries. If a domain list is configured and the
// query domain matches, it resolves via the forwarder, extracts A/AAAA
// addresses into the address-list queue, and returns the response. If the
// domain does not match (or no domain list is configured), the query is
// passed through to the next plugin.
func (m *Mikrotik) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	qname := r.Question[0].Name

	// Domain-list match path.
	if m.domainList != nil && m.domainList.Match(qname) {
		var resp *dns.Msg
		var err error
		if m.exchange != nil {
			resp, err = m.exchange(ctx, r)
		} else {
			resp, err = m.ResolveWithForward(ctx, r)
		}
		if err != nil {
			return dns.RcodeServerFailure, err
		}

		// Enqueue A/AAAA addresses if writers are configured.
		if len(m.writers) > 0 && resp != nil {
			for _, ans := range resp.Answer {
				var addr string
				var mask int
				switch rr := ans.(type) {
				case *dns.A:
					addr = rr.A.String()
					mask = m.mask4
				case *dns.AAAA:
					addr = rr.AAAA.String()
					mask = m.mask6
				default:
					continue
				}
				for _, dw := range m.writers {
					list := ""
					if ip := net.ParseIP(addr); ip != nil && ip.To4() != nil {
						list = dw.cfg.AddressList4
					} else {
						list = dw.cfg.AddressList6
					}
					if list == "" {
						continue
					}
					select {
					case dw.queue <- writeItem{address: addr, list: list, mask: mask}:
					default:
						queueDroppedCount.WithLabelValues(dw.cfg.Address).Inc()
					}
				}
			}
		}

		w.WriteMsg(resp)
		return dns.RcodeSuccess, nil
	}

	// Non-matching path: pass through to the next plugin.
	return m.Next.ServeDNS(ctx, w, r)
}

// ensure Mikrotik implements plugin.Handler.
var _ plugin.Handler = (*Mikrotik)(nil)
