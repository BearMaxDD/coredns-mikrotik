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

		if len(m.writers) > 0 && resp != nil {
			m.enqueueAddresses(resp, route)
		}

		w.WriteMsg(resp)
		return dns.RcodeSuccess, nil
	}

	// No route matched — pass through to next plugin.
	return m.Next.ServeDNS(ctx, w, r)
}

// enqueueAddresses extracts A and AAAA addresses from the response and writes
// them to the MikroTik devices' queues using the RouteRule's address-list and
// mask settings.
func (m *Mikrotik) enqueueAddresses(resp *dns.Msg, route RouteRule) {
	for _, ans := range resp.Answer {
		var addr, list string
		var mask int
		switch rr := ans.(type) {
		case *dns.A:
			addr = rr.A.String()
			list = route.AddressList4
			mask = route.Mask4
		case *dns.AAAA:
			addr = rr.AAAA.String()
			list = route.AddressList6
			mask = route.Mask6
		default:
			continue
		}
		if list == "" || addr == "" {
			continue
		}
		if mask < 0 {
			mask = 0
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

// ensure Mikrotik implements plugin.Handler.
var _ plugin.Handler = (*Mikrotik)(nil)
