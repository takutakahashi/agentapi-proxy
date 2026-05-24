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
		"api.github.com",
		"raw.githubusercontent.com",
		"registry.npmjs.org",
	}
	for _, host := range bypassed {
		if r := f.Check(host); r != FilterResultBypassed {
			t.Errorf("Check(%q) = %v, want bypassed", host, r)
		}
	}
}
