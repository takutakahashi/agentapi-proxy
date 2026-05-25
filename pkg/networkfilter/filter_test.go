package networkfilter

import "testing"

func TestFilterIsDenied(t *testing.T) {
	f := NewFilter([]string{"bad.com", "*.evil.org", "exact.io"})

	cases := []struct {
		host    string
		blocked bool
	}{
		{"bad.com", true},
		{"bad.com:80", true},
		{"good.com", false},
		{"sub.evil.org", true},
		{"evil.org", true},
		{"notevil.org", false},
		{"exact.io", true},
		{"sub.exact.io", false},
		{"", false},
	}
	for _, c := range cases {
		got := f.IsDenied(c.host)
		if got != c.blocked {
			t.Errorf("IsDenied(%q) = %v, want %v", c.host, got, c.blocked)
		}
	}
}

func TestMatchDomainMiddleWildcard(t *testing.T) {
	cases := []struct {
		host    string
		pattern string
		want    bool
	}{
		{"bedrock.us-east-1.amazonaws.com", "bedrock.*.amazonaws.com", true},
		{"bedrock.ap-northeast-1.amazonaws.com", "bedrock.*.amazonaws.com", true},
		{"bedrock-runtime.us-east-1.amazonaws.com", "bedrock-runtime.*.amazonaws.com", true},
		{"bedrock-agent.eu-west-1.amazonaws.com", "bedrock-agent.*.amazonaws.com", true},
		{"s3.us-east-1.amazonaws.com", "bedrock.*.amazonaws.com", false},
		{"notbedrock.us-east-1.amazonaws.com", "bedrock.*.amazonaws.com", false},
		{"bedrock-mantle.us-east-1.api.aws", "bedrock-mantle.*.api.aws", true},
		{"bedrock-mantle.ap-northeast-1.api.aws", "bedrock-mantle.*.api.aws", true},
		{"bedrock-mantle.eu-west-1.api.aws", "bedrock-mantle.*.api.aws", true},
		{"other.us-east-1.api.aws", "bedrock-mantle.*.api.aws", false},
	}
	for _, c := range cases {
		got := matchDomain(c.host, c.pattern)
		if got != c.want {
			t.Errorf("matchDomain(%q, %q) = %v, want %v", c.host, c.pattern, got, c.want)
		}
	}
}

func TestAllowlistFilterEmptyDeniesAll(t *testing.T) {
	f := NewAllowlistFilter(nil)
	cases := []string{"example.com", "good.com", "anything.io", "sub.example.com"}
	for _, host := range cases {
		if r := f.Check(host); r != FilterResultBlocked {
			t.Errorf("NewAllowlistFilter(nil).Check(%q) = %v, want blocked", host, r)
		}
	}
}

func TestDenylistFilterEmptyAllowsAll(t *testing.T) {
	f := NewFilter(nil)
	cases := []string{"example.com", "anything.io", "sub.example.com"}
	for _, host := range cases {
		if r := f.Check(host); r != FilterResultAllowed {
			t.Errorf("NewFilter(nil).Check(%q) = %v, want allowed", host, r)
		}
	}
}

func TestBypassDomains(t *testing.T) {
	f := NewAllowlistFilter([]string{"example.com"})
	bypassed := []string{
		"api.anthropic.com",
		"api.openai.com",
		"bedrock.us-east-1.amazonaws.com",
		"bedrock-runtime.ap-northeast-1.amazonaws.com",
		"bedrock-mantle.us-east-1.api.aws",
		"bedrock-mantle.ap-northeast-1.api.aws",
	}
	for _, host := range bypassed {
		if r := f.Check(host); r != FilterResultBypassed {
			t.Errorf("Check(%q) = %v, want bypassed", host, r)
		}
	}
}

func TestRulesFilter(t *testing.T) {
	// Scenario: deny all → allow api.github.com → deny specific.bad.com
	rules := []FilterRule{
		{Action: "deny", Domains: []string{"*"}},
		{Action: "allow", Domains: []string{"api.github.com", "*.npm.com"}},
		{Action: "deny", Domains: []string{"specific.bad.com"}},
	}
	f := NewRulesFilter(rules)

	cases := []struct {
		host string
		want FilterResult
	}{
		// matched by deny * first, then allow overrides
		{"api.github.com", FilterResultAllowed},
		{"pkg.npm.com", FilterResultAllowed},
		// deny * matches, no allow overrides
		{"example.com", FilterResultBlocked},
		// allow matches, but then deny specific.bad.com overrides
		{"specific.bad.com", FilterResultBlocked},
		// bypass domains always pass
		{"api.anthropic.com", FilterResultBypassed},
		// no-match domain → default deny
		{"unknown.io", FilterResultBlocked},
	}
	for _, c := range cases {
		got := f.Check(c.host)
		if got != c.want {
			t.Errorf("RulesFilter.Check(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestRulesFilterDefaultDeny(t *testing.T) {
	// Empty rules: everything blocked (default deny)
	f := NewRulesFilter(nil)
	for _, host := range []string{"example.com", "good.com", "api.github.com"} {
		if r := f.Check(host); r != FilterResultBlocked {
			t.Errorf("NewRulesFilter(nil).Check(%q) = %v, want blocked", host, r)
		}
	}
}

func TestFormerBypassDomainsNowBlocked(t *testing.T) {
	f := NewAllowlistFilter([]string{"example.com"})
	blocked := []string{
		"github.com",
		"api.github.com",
		"raw.githubusercontent.com",
		"registry.npmjs.org",
		"registry-1.docker.io",
		"hub.docker.com",
	}
	for _, host := range blocked {
		if r := f.Check(host); r == FilterResultBypassed {
			t.Errorf("Check(%q) = bypassed, want blocked or allowed-by-policy", host)
		}
	}
}
