package urlutil_test

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/urlutil"
)

func TestRewriteEncodedSlashes(t *testing.T) {
	tests := []struct {
		rawPath  string
		want     string
		wantOK   bool
	}{
		{"/settings/our%2Fcc-users/sync/push", "/settings/our\x01cc-users/sync/push", true},
		{"/settings/our%2fcc-users/sync/push", "/settings/our\x01cc-users/sync/push", true},
		{"/settings/a%2Fb%2Fc/sync/push", "/settings/a\x01b\x01c/sync/push", true},
		{"/settings/simple-name/sync/push", "/settings/simple-name/sync/push", false},
		{"", "", false},
		{"/settings/no-slash", "/settings/no-slash", false},
	}
	for _, tc := range tests {
		got, ok := urlutil.RewriteEncodedSlashes(tc.rawPath)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("RewriteEncodedSlashes(%q) = (%q, %v), want (%q, %v)",
				tc.rawPath, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestDecodeSlashParam(t *testing.T) {
	tests := []struct {
		param string
		want  string
	}{
		{"our\x01cc-users", "our/cc-users"},
		{"a\x01b\x01c", "a/b/c"},
		{"simple-name", "simple-name"},
		{"", ""},
	}
	for _, tc := range tests {
		got := urlutil.DecodeSlashParam(tc.param)
		if got != tc.want {
			t.Errorf("DecodeSlashParam(%q) = %q, want %q", tc.param, got, tc.want)
		}
	}
}
