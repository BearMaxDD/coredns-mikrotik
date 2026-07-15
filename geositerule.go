package mikrotik

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/BearMaxDD/coredns-mikrotik/geosite"
	"google.golang.org/protobuf/proto"
)

// GeoSiteMatcher implements RuleMatcher from a V2Ray geosite.dat file.
// Domain types:
//   - Plain:   substring match (e.g., "google" matches "google.com", "googleapis.com")
//   - Full:    exact match only (e.g., "example.com" only matches "example.com", not "sub.example.com")
//   - RootDomain: domain + all subdomains (e.g., "example.com" matches "sub.example.com")
//   - Regex:   rejected at load (not supported)
// Thread-safe: Match holds only a read lock.
type GeoSiteMatcher struct {
	mu          sync.RWMutex
	full        map[string]struct{}   // Full domains: exact match only
	rootDomains map[string]struct{}   // RootDomain: parent walk
	plain       []string              // Plain: substring match (lowercase)
}

var _ RuleMatcher = (*GeoSiteMatcher)(nil)

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

	gm := &GeoSiteMatcher{
		full:        make(map[string]struct{}),
		rootDomains: make(map[string]struct{}),
	}

	for _, d := range matched.GetDomain() {
		val := strings.TrimSpace(d.GetValue())
		if val == "" {
			continue
		}
		switch d.GetType() {
		case geosite.Domain_Full:
			// exact match, FQDN
			if !strings.HasSuffix(val, ".") {
				val += "."
			}
			gm.full[strings.ToLower(val)] = struct{}{}

		case geosite.Domain_RootDomain:
			// domain + all subdomains
			val = strings.TrimPrefix(val, ".")
			if !strings.HasSuffix(val, ".") {
				val += "."
			}
			gm.rootDomains[strings.ToLower(val)] = struct{}{}

		case geosite.Domain_Plain:
			// substring match (keyword)
			gm.plain = append(gm.plain, strings.ToLower(val))

		case geosite.Domain_Regex:
			return nil, fmt.Errorf("geosite: regex domain type not supported: %q", val)
		}
	}

	return gm, nil
}

// Match checks the domain against stored geosite entries.
// Order: full exact → root domain parent walk → plain substring.
func (gm *GeoSiteMatcher) Match(domain string) bool {
	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}
	domain = strings.ToLower(domain)
	original := domain // 保留用于 Plain 匹配

	gm.mu.RLock()
	defer gm.mu.RUnlock()

	// 1. Full: exact match only
	if _, ok := gm.full[domain]; ok {
		return true
	}

	// 2. RootDomain: parent walk on a copy
	candidate := domain
	for {
		if _, ok := gm.rootDomains[candidate]; ok {
			return true
		}
		dot := strings.IndexByte(candidate, '.')
		if dot < 0 {
			break
		}
		candidate = candidate[dot+1:]
	}

	// 3. Plain: substring match against original domain
	for _, keyword := range gm.plain {
		if strings.Contains(original, keyword) {
			return true
		}
	}

	return false
}

func (gm *GeoSiteMatcher) Close() {}
