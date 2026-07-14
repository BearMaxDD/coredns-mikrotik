package mikrotik

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/miekg/dns"
)

// Name implements plugin.Handler.
func (m *Mikrotik) Name() string { return "mikrotik" }

// ServeDNS intercepts DNS queries. It iterates over routes and on the first
// matching route resolves via the forwarder, extracts A/AAAA addresses into
// the address-list queue, and returns the response. If no route matches, the
// query is passed through to the next plugin.
func (m *Mikrotik) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	qname := r.Question[0].Name

	for _, route := range m.routes {
		if route.Matcher == nil || !route.Matcher.Match(qname) {
			continue
		}

		// Route matched! Determine forward address.
		forwardAddr := route.Forward
		if forwardAddr == "" {
			forwardAddr = m.listForward
		}

		resp, err := m.resolveWithForward(ctx, r, forwardAddr)
		if err != nil {
			return dns.RcodeServerFailure, err
		}

		// Enqueue A/AAAA addresses if writers are configured.
		if len(m.writers) > 0 && resp != nil {
			for _, ans := range resp.Answer {
				var addr string
				var mask int
				var list string
				switch rr := ans.(type) {
				case *dns.A:
					addr = rr.A.String()
					mask = route.Mask4
					if mask < 0 {
						mask = 0
					}
					list = route.AddressList4
					if list == "" && len(m.writers) > 0 {
						list = m.writers[0].cfg.AddressList4
					}
				case *dns.AAAA:
					addr = rr.AAAA.String()
					mask = route.Mask6
					if mask < 0 {
						mask = 0
					}
					list = route.AddressList6
					if list == "" && len(m.writers) > 0 {
						list = m.writers[0].cfg.AddressList6
					}
				default:
					continue
				}
				if list == "" {
					continue
				}
				for _, dw := range m.writers {
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

	// No route matched — pass through to next plugin.
	return m.Next.ServeDNS(ctx, w, r)
}

// ensure Mikrotik implements plugin.Handler.
var _ plugin.Handler = (*Mikrotik)(nil)
