package networkfilter

import (
	"strings"
)

// bypassDomains are always allowed regardless of allowlist/denylist mode.
// These are required for Claude Code sessions to function correctly:
// - anthropic.com: Claude API
// - svc.cluster.local: Kubernetes internal services (stop hooks, health checks)
// - github.com / githubusercontent.com: GitHub auth, raw content, API
// - storage.googleapis.com: tool downloads via GCS
// - sentry.io: error reporting
// - npmjs.org: npm package registry (tool installation)
// - docker.io / docker.com: Docker Hub image pulls
// - openai.com: codex-acp OpenAI backend
// - bedrock.*.amazonaws.com: AWS Bedrock (region-scoped endpoints)
var bypassDomains = normalize([]string{
	"*.anthropic.com",
	"anthropic.com",
	"*.svc.cluster.local",
	"github.com",
	"*.github.com",
	"*.githubusercontent.com",
	"storage.googleapis.com",
	"sentry.io",
	"*.sentry.io",
	"registry.npmjs.org",
	"*.docker.io",
	"*.docker.com",
	"api.openai.com",
	"bedrock.*.amazonaws.com",
	"bedrock-runtime.*.amazonaws.com",
	"bedrock-agent.*.amazonaws.com",
	"bedrock-agent-runtime.*.amazonaws.com",
})

// Filter decides whether a given host should be blocked.
// When allowedDomains is non-empty, only those domains pass (allowlist mode).
// Otherwise, deniedDomains are blocked (denylist mode).
// bypassDomains are always allowed regardless of mode.
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

// FilterResult describes the outcome of a filter check.
type FilterResult int

const (
	FilterResultAllowed  FilterResult = iota // passed allowlist/denylist check
	FilterResultBypassed                     // always-allow bypass domain
	FilterResultBlocked                      // denied by allowlist or denylist
)

func (r FilterResult) String() string {
	switch r {
	case FilterResultBypassed:
		return "bypassed"
	case FilterResultBlocked:
		return "blocked"
	default:
		return "allowed"
	}
}

// Check returns the FilterResult for the given host.
// host may include a port suffix (host:port); it is stripped before matching.
func (f *Filter) Check(host string) FilterResult {
	h := strings.ToLower(host)
	// Strip port if present.
	if idx := strings.LastIndex(h, ":"); idx != -1 {
		if strings.Contains(h[idx:], ":") || !strings.Contains(h, "[") {
			h = h[:idx]
		}
	}
	h = strings.TrimSuffix(h, ".")

	// Bypass check: always allow regardless of mode.
	for _, bypass := range bypassDomains {
		if matchDomain(h, bypass) {
			return FilterResultBypassed
		}
	}

	// Allowlist mode: deny everything NOT in the allowed list.
	if len(f.allowedDomains) > 0 {
		for _, allowed := range f.allowedDomains {
			if matchDomain(h, allowed) {
				return FilterResultAllowed
			}
		}
		return FilterResultBlocked
	}

	// Denylist mode: deny only matched domains.
	for _, denied := range f.deniedDomains {
		if matchDomain(h, denied) {
			return FilterResultBlocked
		}
	}
	return FilterResultAllowed
}

// IsDenied returns true when host should be blocked.
func (f *Filter) IsDenied(host string) bool {
	return f.Check(host) == FilterResultBlocked
}

// matchDomain checks whether host matches the pattern.
// Supported wildcard forms:
//   - "*.example.com" — any subdomain of example.com (leading wildcard)
//   - "prefix.*.example.com" — prefix + any single label + suffix (middle wildcard)
func matchDomain(host, pattern string) bool {
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return host == pattern[2:] || strings.HasSuffix(host, suffix)
	}
	// Middle wildcard: e.g. "bedrock.*.amazonaws.com"
	if idx := strings.Index(pattern, "*."); idx > 0 {
		prefix := pattern[:idx]  // "bedrock."
		suffix := pattern[idx+1:] // ".amazonaws.com"
		return strings.HasPrefix(host, prefix) && strings.HasSuffix(host, suffix)
	}
	return host == pattern
}
