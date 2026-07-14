package mikrotik

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/netctrldns/coredns-mikrotik/geosite"
	"google.golang.org/protobuf/proto"
)

// GeoSiteMatcher implements RuleMatcher from a V2Ray geosite.dat file.
// Thread-safe: Match holds only a read lock.
type GeoSiteMatcher struct {
	mu      sync.RWMutex
	domains map[string]struct{}
}

var _ RuleMatcher = (*GeoSiteMatcher)(nil)

// NewGeoSiteMatcher reads a geosite.dat file and extracts all domains matching
// the given country code. Plain, Full, and RootDomain entries are stored for
// suffix-based matching (parent-domain walk). Regex entries are skipped.
func NewGeoSiteMatcher(path, code string) (*GeoSiteMatcher, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var list geosite.GeoSiteList
	if err := proto.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("geosite: unmarshal %s: %w", path, err)
	}

	code = strings.ToLower(code)
	var matched *geosite.GeoSite
	for _, site := range list.Entry {
		if site != nil && strings.ToLower(site.GetCountryCode()) == code {
			matched = site
			break
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("geosite: code %q not found in %s", code, path)
	}

	domains := make(map[string]struct{})
	for _, d := range matched.GetDomain() {
		val := strings.TrimSpace(d.GetValue())
		if val == "" {
			continue
		}
		switch d.GetType() {
		case geosite.Domain_Plain, geosite.Domain_Full:
			if !strings.HasSuffix(val, ".") {
				val += "."
			}
			domains[strings.ToLower(val)] = struct{}{}
		case geosite.Domain_RootDomain:
			val = strings.TrimPrefix(val, ".")
			if !strings.HasSuffix(val, ".") {
				val += "."
			}
			val = strings.ToLower(val)
			domains[val] = struct{}{}
		case geosite.Domain_Regex:
			// skip regex for now
		}
	}

	return &GeoSiteMatcher{domains: domains}, nil
}

// Match checks if domain or any parent domain is in the stored domain set.
// Input is normalized (lowercase, trailing dot added if missing).
func (gm *GeoSiteMatcher) Match(domain string) bool {
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}
	domain = strings.ToLower(domain)
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	for {
		if _, ok := gm.domains[domain]; ok {
			return true
		}
		dot := strings.IndexByte(domain, '.')
		if dot < 0 {
			return false
		}
		domain = domain[dot+1:]
	}
}

// Close implements RuleMatcher. No-op for GeoSiteMatcher.
func (gm *GeoSiteMatcher) Close() {}
