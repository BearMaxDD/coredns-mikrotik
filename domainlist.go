package mikrotik

import (
	"bufio"
	"context"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"time"
)

// DomainList manages a domain list loaded from a text file, with optional
// periodic reload. Thread-safe.
type DomainList struct {
	mu      sync.RWMutex
	domains map[string]struct{}
	cancel  context.CancelFunc
}

// NewDomainList creates a DomainList and loads domains from path.
// If reloadInterval > 0, a background goroutine reloads the file periodically
// with +-30% jitter. Otherwise the file is loaded once.
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

// Match reports whether domain is in the list.
// The trailing dot is appended automatically if missing, and comparison is
// case-insensitive.
func (dl *DomainList) Match(domain string) bool {
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}
	domain = strings.ToLower(domain)
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	_, ok := dl.domains[domain]
	return ok
}

// Close stops the background reload goroutine (if any) and releases resources.
func (dl *DomainList) Close() {
	if dl.cancel != nil {
		dl.cancel()
	}
}

// load reads the file at path and replaces the domain set. Lines starting
// with "#" or empty lines are skipped. Domains are lowercased and the trailing
// dot is ensured.
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

// reloadLoop periodically reloads the file with +-30% jitter on the interval.
func (dl *DomainList) reloadLoop(ctx context.Context, path string, interval time.Duration) {
	baseNanos := float64(interval.Nanoseconds())
	jitter := time.Duration(baseNanos * (0.7 + rand.Float64()*0.6))
	ticker := time.NewTicker(jitter)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			jitter = time.Duration(baseNanos * (0.7 + rand.Float64()*0.6))
			ticker.Reset(jitter)
			if err := dl.load(path); err != nil {
				// log only, never panic
			}
		case <-ctx.Done():
			return
		}
	}
}
