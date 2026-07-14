package mikrotik

import (
	"bufio"
	"context"
	"log"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"time"
)

// RuleMatcher is the interface for domain matching.
// Implementations must be thread-safe.
type RuleMatcher interface {
	Match(domain string) bool
	Close()
}

// RouteRule defines a single route: how to forward and which address-list to write.
// Empty Forward means use global config.
// Empty AddressList4/6 means use global config.
// Mask4/Mask6 == -1 means use global config.
type RouteRule struct {
	Matcher      RuleMatcher
	Forward      string
	AddressList4 string
	AddressList6 string
	Mask4        int
	Mask6        int
}

// DomainList implements RuleMatcher from a text file. Thread-safe.
// Subdomain matching with label boundary.
type DomainList struct {
	mu      sync.RWMutex
	domains map[string]struct{}
	cancel  context.CancelFunc
}

var _ RuleMatcher = (*DomainList)(nil)

func NewDomainList(path string, reloadInterval time.Duration) (*DomainList, error) {
	dl := &DomainList{}
	if err := dl.load(path); err != nil {
		return nil, err
	}
	if reloadInterval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		dl.cancel = cancel
		go dl.reloadLoop(ctx, path, reloadInterval)
	}
	return dl, nil
}

// Match checks if domain or any parent domain is in the list,
// with label boundary: "sub.example.com." matches "example.com."
// but "badexample.com." does not.
func (dl *DomainList) Match(domain string) bool {
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}
	domain = strings.ToLower(domain)

	dl.mu.RLock()
	defer dl.mu.RUnlock()

	for {
		if _, ok := dl.domains[domain]; ok {
			return true
		}
		dot := strings.IndexByte(domain, '.')
		if dot < 0 {
			return false
		}
		domain = domain[dot+1:]
	}
}

func (dl *DomainList) Close() {
	if dl.cancel != nil {
		dl.cancel()
	}
}

func (dl *DomainList) load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	domains := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.ToLower(line)
		if !strings.HasSuffix(line, ".") {
			line += "."
		}
		domains[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	dl.mu.Lock()
	dl.domains = domains
	dl.mu.Unlock()
	return nil
}

func (dl *DomainList) reloadLoop(ctx context.Context, path string, interval time.Duration) {
	baseNanos := float64(interval.Nanoseconds())
	calcJitter := func() time.Duration {
		return time.Duration(baseNanos * (0.7 + rand.Float64()*0.6))
	}
	ticker := time.NewTicker(calcJitter())
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ticker.Reset(calcJitter())
			if err := dl.load(path); err != nil {
				log.Printf("mikrotik: domain list reload failed: %v (keeping previous list)", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
