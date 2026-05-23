package networkfilter

import (
	"strings"
)

// Filter decides whether a given host should be blocked based on the denied domain list.
type Filter struct {
	deniedDomains []string
}

// NewFilter creates a new Filter with the given list of denied domains.
// Each entry may be an exact hostname or a wildcard prefix (e.g. "*.example.com").
func NewFilter(deniedDomains []string) *Filter {
	normalized := make([]string, 0, len(deniedDomains))
	for _, d := range deniedDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			normalized = append(normalized, d)
		}
	}
	return &Filter{deniedDomains: normalized}
}

// IsDenied returns true when host matches a denied domain.
// host may include a port suffix (host:port); it is stripped before matching.
func (f *Filter) IsDenied(host string) bool {
	h := strings.ToLower(host)
	// Strip port if present.
	if idx := strings.LastIndex(h, ":"); idx != -1 {
		// Make sure it's not an IPv6 bracket address without port.
		if strings.Contains(h[idx:], ":") || !strings.Contains(h, "[") {
			h = h[:idx]
		}
	}
	h = strings.TrimSuffix(h, ".")
	for _, denied := range f.deniedDomains {
		if matchDomain(h, denied) {
			return true
		}
	}
	return false
}

// matchDomain checks whether host matches the pattern.
// pattern may start with "*." for wildcard subdomain matching.
func matchDomain(host, pattern string) bool {
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return host == pattern[2:] || strings.HasSuffix(host, suffix)
	}
	return host == pattern
}
