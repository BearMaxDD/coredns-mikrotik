package mikrotik

import (
	"context"
	"net"

	"github.com/miekg/dns"
)

// mikrotikResponseWriter wraps dns.ResponseWriter to capture the written response
// for address-list extraction.
type mikrotikResponseWriter struct {
	dns.ResponseWriter
	resp    *dns.Msg
	written bool
}

// WriteMsg captures the response before forwarding to the actual writer.
func (w *mikrotikResponseWriter) WriteMsg(resp *dns.Msg) error {
	w.resp = resp
	w.written = true
	return w.ResponseWriter.WriteMsg(resp)
}

// ServeDNS intercepts DNS queries, chains to the next plugin, then enqueues
// address-list update items for any A or AAAA records in the response.
func (m *Mikrotik) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	wrapper := &mikrotikResponseWriter{ResponseWriter: w}
	rcode, err := m.Next.ServeDNS(ctx, wrapper, r)

	if !wrapper.written || wrapper.resp == nil {
		return rcode, err
	}

	for _, ans := range wrapper.resp.Answer {
		var addr string
		switch rr := ans.(type) {
		case *dns.A:
			addr = rr.A.String()
		case *dns.AAAA:
			addr = rr.AAAA.String()
		default:
			continue
		}
		list := listForAddr(addr, m.writers)
		if list == "" {
			continue
		}
		for _, dw := range m.writers {
			if dw.cfg.AddressList4 != list && dw.cfg.AddressList6 != list {
				continue
			}
			select {
			case dw.queue <- writeItem{address: addr, list: list}:
		default:
			queueDroppedCount.WithLabelValues(dw.cfg.Address).Inc()
			}
		}
	}
	return rcode, err
}

// listForAddr returns the address-list name for the given address, searching
// through all device writers. IPv4 addresses prefer AddressList4; everything
// else (IPv6, invalid) checks AddressList6.
func listForAddr(addr string, writers []*deviceWriter) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return ""
	}
	if ip.To4() != nil {
		for _, dw := range writers {
			if dw.cfg.AddressList4 != "" {
				return dw.cfg.AddressList4
			}
		}
	}
	for _, dw := range writers {
		if dw.cfg.AddressList6 != "" {
			return dw.cfg.AddressList6
		}
	}
	return ""
}
