package networkfilter

import "testing"

func TestProxyCountModeRecordsButDoesNotBlock(t *testing.T) {
	proxy := NewProxy(NewAllowlistFilter([]string{"allowed.example.com"}), true, true)

	result := proxy.effectiveFilter().Check("blocked.example.com")
	if result != FilterResultBlocked {
		t.Fatalf("Check() = %s, want blocked", result)
	}
	if proxy.shouldBlock(result) {
		t.Fatalf("shouldBlock(blocked) = true, want false in count mode")
	}
}

func TestProxySetPolicyUpdatesActiveFilterAndCountMode(t *testing.T) {
	proxy := NewProxy(NewFilter(nil), true, false)
	proxy.SetPolicy(NewAllowlistFilter([]string{"allowed.example.com"}), true)

	result := proxy.effectiveFilter().Check("blocked.example.com")
	if result != FilterResultBlocked {
		t.Fatalf("Check() = %s, want blocked", result)
	}
	if proxy.shouldBlock(result) {
		t.Fatalf("shouldBlock(blocked) = true, want false after enabling count mode")
	}
}
