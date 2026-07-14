package mikrotik

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
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
	mask    int
}

// deviceWriter manages address-list writes to a single MikroTik device.
type deviceWriter struct {
	cfg    DeviceConfig
	queue  chan writeItem
	dedup  sync.Map
	client rosClient
	stop   chan struct{}
	started atomic.Bool
}

// Mikrotik is the CoreDNS plugin that manages MikroTik address-list entries.
type Mikrotik struct {
	Next        plugin.Handler
	writers     []*deviceWriter
	domainList  DomainMatcher
	listForward string
	mask4       int
	mask6       int

	// exchange, if set, is used by ServeDNS instead of ResolveWithForward.
	// Used for testing; nil in production.
	exchange func(ctx context.Context, r *dns.Msg) (*dns.Msg, error)
}


// ResolveWithForward forwards a DNS query to the configured upstream and returns the response.
func (m *Mikrotik) ResolveWithForward(ctx context.Context, r *dns.Msg) (*dns.Msg, error) {
	c := new(dns.Client)
	r.RecursionDesired = true
	resp, _, err := c.ExchangeContext(ctx, r, m.listForward)
	return resp, err
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
	var domainsFilePath string
	var reloadInterval time.Duration

	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "domains-file":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				domainsFilePath = args[0]

			case "reload":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				d, err := time.ParseDuration(args[0])
				if err != nil {
					return nil, fmt.Errorf("invalid reload duration %q: %v", args[0], err)
				}
				reloadInterval = d

			case "mask4":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 0 || n > 32 {
					return nil, fmt.Errorf("invalid mask4 %q: must be 0-32", args[0])
				}
				m.mask4 = n

			case "mask6":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 0 || n > 128 {
					return nil, fmt.Errorf("invalid mask6 %q: must be 0-128", args[0])
				}
				m.mask6 = n

			case "forward":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				m.listForward = args[0]

			case "device":
				args := c.RemainingArgs()
				if len(args) != 3 {
					return nil, c.ArgErr()
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
					return nil, fmt.Errorf("address-list4 requires a device block")
				}
				current.cfg.AddressList4 = args[0]

			case "address-list6":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				if !seenDevice {
					return nil, fmt.Errorf("address-list6 requires a device block")
				}
				current.cfg.AddressList6 = args[0]

			case "timeout":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				if !seenDevice {
					return nil, fmt.Errorf("timeout requires a device block")
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
					return nil, fmt.Errorf("comment requires a device block")
				}
				current.cfg.Comment = args[0]

			default:
				return nil, c.Errf("unknown property '%s'", c.Val())
			}
		}
	}

	if domainsFilePath == "" {
		return nil, fmt.Errorf("domains-file is required")
	}

	dl, err := NewDomainList(domainsFilePath, reloadInterval)
	if err != nil {
		return nil, fmt.Errorf("loading domains-file: %v", err)
	}
	m.domainList = dl

	return m, nil
}

// run starts the worker loop for this device writer.
func (dw *deviceWriter) run() {
	dw.started.Store(true)
	defer dw.started.Store(false)
	defer dw.closeClient()

	for {
		select {
		case <-dw.stop:
			drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			dw.drain(drainCtx)
			cancel()
			return
		case item := <-dw.queue:
			dw.processItem(context.Background(), item)
		}
	}
}
