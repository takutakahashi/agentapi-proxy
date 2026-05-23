package networkfilter

import (
	"strings"
)

// Filter decides whether a given host should be blocked.
// When allowedDomains is non-empty, only those domains pass (allowlist mode).
// Otherwise, deniedDomains are blocked (denylist mode).
type Filter struct {
	deniedDomains  []string
	allowedDomains []string
}

func normalize(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// NewFilter creates a denylist filter.
func NewFilter(deniedDomains []string) *Filter {
	return &Filter{deniedDomains: normalize(deniedDomains)}
}

// NewAllowlistFilter creates an allowlist filter: only listed domains are permitted.
func NewAllowlistFilter(allowedDomains []string) *Filter {
	return &Filter{allowedDomains: normalize(allowedDomains)}
}

// IsDenied returns true when host should be blocked.
// host may include a port suffix (host:port); it is stripped before matching.
func (f *Filter) IsDenied(host string) bool {
	h := strings.ToLower(host)
	// Strip port if present.
	if idx := strings.LastIndex(h, ":"); idx != -1 {
		if strings.Contains(h[idx:], ":") || !strings.Contains(h, "[") {
			h = h[:idx]
		}
	}
	h = strings.TrimSuffix(h, ".")

	// Allowlist mode: deny everything NOT in the allowed list.
	if len(f.allowedDomains) > 0 {
		for _, allowed := range f.allowedDomains {
			if matchDomain(h, allowed) {
				return false
			}
		}
		return true
	}

	// Denylist mode: deny only matched domains.
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
