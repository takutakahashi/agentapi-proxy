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
