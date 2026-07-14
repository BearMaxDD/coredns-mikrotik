package mikrotik

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/miekg/dns"
)

// DeviceConfig holds the configuration for a single MikroTik device.
type DeviceConfig struct {
	Address      string
	Username     string
	Password     string
	AddressList4 string
	AddressList6 string
	Timeout      time.Duration
	Comment      string
}

// writeItem represents an address-list write request.
type writeItem struct {
	address string
	list    string
}

// deviceWriter manages address-list writes to a single MikroTik device.
type deviceWriter struct {
	cfg    DeviceConfig
	queue  chan writeItem
	dedup  sync.Map
	client rosClient
	stop   chan struct{}
}

// Mikrotik is the CoreDNS plugin that manages MikroTik address-list entries.
type Mikrotik struct {
	Next    plugin.Handler
	writers []*deviceWriter
}

// Name implements plugin.Handler.
func (m *Mikrotik) Name() string { return "mikrotik" }

// ServeDNS implements plugin.Handler.
func (m *Mikrotik) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	if m.Next != nil {
		return m.Next.ServeDNS(ctx, w, r)
	}
	return dns.RcodeServerFailure, fmt.Errorf("mikrotik: no next plugin")
}

func init() {
	plugin.Register("mikrotik", setup)
}

func setup(c *caddy.Controller) error {
	m, err := parseConfig(c)
	if err != nil {
		return plugin.Error("mikrotik", err)
	}

	for _, w := range m.writers {
		w := w
		c.OnStartup(func() error {
			go w.run()
			return nil
		})
		c.OnShutdown(func() error {
			close(w.stop)
			return nil
		})
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		m.Next = next
		return m
	})

	return nil
}

// parseConfig parses the mikrotik configuration block from Corefile.
func parseConfig(c *caddy.Controller) (*Mikrotik, error) {
	m := &Mikrotik{}
	var current *deviceWriter
	seenDevice := false
	directiveBeforeDevice := false

	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "device":
				args := c.RemainingArgs()
				if len(args) != 3 {
					return nil, c.ArgErr()
				}
				if !seenDevice && directiveBeforeDevice {
				return nil, fmt.Errorf("directives must follow device declarations")
				}
				seenDevice = true
				current = &deviceWriter{
					cfg: DeviceConfig{
						Address:  args[0],
						Username: args[1],
						Password: args[2],
						Timeout:  24 * time.Hour,
					},
				queue: make(chan writeItem, 1024),
					stop:  make(chan struct{}),
				}
				m.writers = append(m.writers, current)

			case "address-list4":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				if !seenDevice {
					directiveBeforeDevice = true
					continue
				}
				current.cfg.AddressList4 = args[0]

			case "address-list6":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				if !seenDevice {
					directiveBeforeDevice = true
					continue
				}
				current.cfg.AddressList6 = args[0]

			case "timeout":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				if !seenDevice {
					directiveBeforeDevice = true
					continue
				}
				d, err := time.ParseDuration(args[0])
				if err != nil {
				return nil, fmt.Errorf("invalid timeout %q: %v", args[0], err)
				}
				current.cfg.Timeout = d

			case "comment":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				if !seenDevice {
					directiveBeforeDevice = true
					continue
				}
				current.cfg.Comment = args[0]

			default:
				return nil, c.Errf("unknown property '%s'", c.Val())
			}
		}
	}

	if !seenDevice {
		return nil, fmt.Errorf("no device configured")
	}

	return m, nil
}

// run starts the worker loop for this device writer.
// Currently a stub — full implementation in a later task.
func (w *deviceWriter) run() {
	<-w.stop
}
