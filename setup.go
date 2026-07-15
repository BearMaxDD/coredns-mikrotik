package mikrotik

import (
	"context"
	"fmt"
	"net"
	"strconv"
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
	RefreshOnHit bool
}

// writeItem represents an address-list write request.
type writeItem struct {
	address string
	list    string
	mask    int
	domain  string
}

type deviceWriter struct {
	cfg         DeviceConfig
	queue       chan writeItem
	wcache      *writeCache
	client      rosClient
	stop        chan struct{}
	started     atomic.Bool
	nextAllowed time.Time
	backoff     time.Duration
}

// Mikrotik is the CoreDNS plugin that manages MikroTik address-list entries.
type Mikrotik struct {
	Next        plugin.Handler
	writers     []*deviceWriter
	routes      []RouteRule
	listForward string
	dryRun      bool
	refreshOnHit bool
	exchange    func(ctx context.Context, r *dns.Msg) (*dns.Msg, error)
}

// resolveWithForward forwards a DNS query to the given upstream address.
// If the address lacks a port, ":53" is appended.
func (m *Mikrotik) resolveWithForward(ctx context.Context, r *dns.Msg, addr string) (*dns.Msg, error) {
	if m.exchange != nil {
		return m.exchange(ctx, r)
	}
	// Normalize: add :53 if no port
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "53")
	}
	c := new(dns.Client)
	r.RecursionDesired = true
	resp, _, err := c.ExchangeContext(ctx, r, addr)
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

	// Close all route matchers on shutdown.
	c.OnShutdown(func() error {
		for _, route := range m.routes {
			if route.Matcher != nil {
				route.Matcher.Close()
			}
		}
		return nil
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		m.Next = next
		return m
	})

	return nil
}

// parseConfig parses the mikrotik configuration block from Corefile.
// Accepted directives:
//
//	device <addr> <user> <pass>               — MikroTik API endpoint (repeatable)
//	timeout <duration>                        — per-device connection timeout
//	comment <text>                            — comment added to address-list entries
//	forward <addr>                            — default upstream DNS resolver
//	address-list4 <name>                      — default IPv4 address-list name
//	address-list6 <name>                      — default IPv6 address-list name
//	mask4 <n>                                 — default IPv4 prefix length (0-32)
//	mask6 <n>                                 — default IPv6 prefix length (0-128)
//	domains-file <path>                       — route using plain domain list
//	reload <duration>                         — domain list reload interval
//	geosite-file <path>                       — path to V2Ray geosite.dat
//	geosite <code> [opt val ...]              — route using geosite country code
//	  Options (one line): address-list4, address-list6, mask4, mask6
func parseConfig(c *caddy.Controller) (*Mikrotik, error) {
	m := &Mikrotik{}
	var current *deviceWriter
	seenDevice := false
	var reloadInterval time.Duration
	var writeCacheTTL time.Duration
	var geositeFilePath string
	// Route-level defaults: routes that don't override use these.
	defaultMask4 := 0
	defaultMask6 := 0
	var defaultRouteForward string
	var defaultAddrList4 string
	var defaultAddrList6 string

	// Deferred route specs preserved in config order.
	// Override fields: empty means "use final default", mask -1 means "not set".
	type routeSpec struct {
		kind        string
		path        string
		reload      time.Duration
		ovAddrList4 string
		ovAddrList6 string
		ovForward   string
		ovMask4     int
		ovMask6     int
		noWrite     bool
	}
	var routeSpecs []routeSpec

	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "dry-run":
				if len(c.RemainingArgs()) > 0 {
					return nil, c.Err("dry-run does not accept arguments")
				}
				m.dryRun = true

			case "refresh-on-hit":
				if len(c.RemainingArgs()) > 0 {
					return nil, c.Err("refresh-on-hit does not accept arguments")
				}
				m.refreshOnHit = true
			case "domains-file":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				rs := routeSpec{
					kind:    "domains",
					path:    c.Val(),
					ovMask4: -1,
					ovMask6: -1,
				}
				for c.NextArg() {
					switch c.Val() {
					case "no-write":
						rs.noWrite = true
					case "forward":
						if !c.NextArg() {
							return nil, c.Errf("domains-file: forward requires an address")
						}
						rs.ovForward = c.Val()
					default:
						return nil, c.Errf("domains-file: unknown option %q", c.Val())
					}
				}
				routeSpecs = append(routeSpecs, rs)
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

			case "write-cache-ttl":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				d, err := time.ParseDuration(args[0])
				if err != nil {
					return nil, fmt.Errorf("invalid write-cache-ttl %q: %v", args[0], err)
				}
				if d <= 0 {
					return nil, fmt.Errorf("write-cache-ttl must be positive, got %v", d)
				}
				writeCacheTTL = d

			case "mask4":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 0 || n > 32 {
					return nil, fmt.Errorf("invalid mask4 %q: must be 0-32", args[0])
				}
				defaultMask4 = n

			case "mask6":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 0 || n > 128 {
					return nil, fmt.Errorf("invalid mask6 %q: must be 0-128", args[0])
				}
				defaultMask6 = n

			case "forward":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				defaultRouteForward = args[0]
				m.listForward = args[0]

			case "address-list4":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				defaultAddrList4 = args[0]
				if seenDevice && current != nil {
					current.cfg.AddressList4 = args[0]
				}

			case "address-list6":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				defaultAddrList6 = args[0]
				if seenDevice && current != nil {
					current.cfg.AddressList6 = args[0]
				}

			case "geosite-file":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, c.ArgErr()
				}
				geositeFilePath = args[0]

			case "geosite":
				args := c.RemainingArgs()
				if len(args) < 1 {
					return nil, c.ArgErr()
				}
				if geositeFilePath == "" {
					return nil, fmt.Errorf("geosite directive requires geosite-file to be set first")
				}

				// Parse remaining args as key-value pairs.
				ovAddrList4 := ""
				ovAddrList6 := ""
				ovForward := ""
				ovMask4 := -1
				ovMask6 := -1
				i := 1
				for i < len(args) {
					switch args[i] {
					case "address-list4":
						i++
						if i >= len(args) {
							return nil, fmt.Errorf("geosite: address-list4 requires a value")
						}
						ovAddrList4 = args[i]
						i++
					case "address-list6":
						i++
						if i >= len(args) {
							return nil, fmt.Errorf("geosite: address-list6 requires a value")
						}
						ovAddrList6 = args[i]
						i++
					case "mask4":
						i++
						if i >= len(args) {
							return nil, fmt.Errorf("geosite: mask4 requires a value")
						}
						n, err := strconv.Atoi(args[i])
						if err != nil || n < 0 || n > 32 {
							return nil, fmt.Errorf("geosite: invalid mask4 %q: must be 0-32", args[i])
						}
						ovMask4 = n
						i++
					case "mask6":
						i++
						if i >= len(args) {
							return nil, fmt.Errorf("geosite: mask6 requires a value")
						}
						n, err := strconv.Atoi(args[i])
						if err != nil || n < 0 || n > 128 {
							return nil, fmt.Errorf("geosite: invalid mask6 %q: must be 0-128", args[i])
						}
						ovMask6 = n
						i++
					default:
						return nil, fmt.Errorf("geosite: unknown option %q", args[i])
					}
				}

				routeSpecs = append(routeSpecs, routeSpec{
					kind:        "geosite",
					path:        args[0],
					ovAddrList4: ovAddrList4,
					ovAddrList6: ovAddrList6,
					ovForward:   ovForward,
					ovMask4:     ovMask4,
					ovMask6:     ovMask6,
				})
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

	// Initialize write cache for all writers with effective TTL.
	for _, dw := range m.writers {
		ttl := effectiveWriteCacheTTL(writeCacheTTL, dw.cfg.Timeout)
		dw.cfg.RefreshOnHit = m.refreshOnHit
		dw.wcache = newWriteCache(ttl)
	}

	// Create all deferred routes in config order, resolving overrides
	// against the final default values.
	for _, rs := range routeSpecs {
		addrList4 := ""
		addrList6 := ""
		if !rs.noWrite {
			addrList4 = defaultAddrList4
			if rs.ovAddrList4 != "" {
				addrList4 = rs.ovAddrList4
			}
			addrList6 = defaultAddrList6
			if rs.ovAddrList6 != "" {
				addrList6 = rs.ovAddrList6
			}
		}

		forwardAddr := defaultRouteForward
		if rs.ovForward != "" {
			forwardAddr = rs.ovForward
		}
		mask4 := defaultMask4
		if rs.ovMask4 >= 0 {
			mask4 = rs.ovMask4
		}
		mask6 := defaultMask6
		if rs.ovMask6 >= 0 {
			mask6 = rs.ovMask6
		}

		switch rs.kind {
		case "domains":
			dl, err := NewDomainList(rs.path, reloadInterval)
			if err != nil {
				return nil, fmt.Errorf("loading domains-file: %v", err)
			}
			m.routes = append(m.routes, RouteRule{
				Matcher:      dl,
				Forward:      forwardAddr,
				AddressList4: addrList4,
				AddressList6: addrList6,
				Mask4:        mask4,
				Mask6:        mask6,
			})
		case "geosite":
			gm, err := NewGeoSiteMatcher(geositeFilePath, rs.path)
			if err != nil {
				return nil, fmt.Errorf("geosite %s: %v", rs.path, err)
			}
			m.routes = append(m.routes, RouteRule{
				Matcher:      gm,
				Forward:      forwardAddr,
				AddressList4: addrList4,
				AddressList6: addrList6,
				Mask4:        mask4,
				Mask6:        mask6,
			})
		}
	}

	if len(m.routes) == 0 && len(m.writers) == 0 {
		return nil, fmt.Errorf("at least one of: domains-file, geosite, or device is required")
	}

	return m, nil
}

// effectiveWriteCacheTTL computes the effective write-cache TTL.
// configured=0 → default: min(timeout/2, 1h). Non-zero → capped at timeout.
func effectiveWriteCacheTTL(configured, timeout time.Duration) time.Duration {
	if configured > 0 {
		if timeout > 0 && configured > timeout {
			return timeout
		}
		return configured
	}
	// Default
	d := time.Hour
	if timeout > 0 && timeout/2 < d {
		d = timeout / 2
	}
	return d
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
