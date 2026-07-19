package app

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestExternalSessionManagerMatchesAllocatorTags(t *testing.T) {
	manager := entities.ExternalSessionManagerEntry{
		ID: "native-1",
		Labels: map[string]string{
			"os":   "linux",
			"arch": "amd64",
			"pool": "developers",
		},
	}
	tests := []struct {
		name string
		tags map[string]string
		want bool
	}{
		{name: "no selector", tags: map[string]string{"repository": "owner/repo"}, want: true},
		{name: "all selectors match", tags: map[string]string{"allocator.os": "linux", "allocator.pool": "developers"}, want: true},
		{name: "allocator ID", tags: map[string]string{"allocator.id": "native-1"}, want: true},
		{name: "missing label", tags: map[string]string{"allocator.location": "tokyo"}, want: false},
		{name: "case sensitive", tags: map[string]string{"allocator.os": "Linux"}, want: false},
		{name: "ID mismatch", tags: map[string]string{"allocator.id": "native-2"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := externalSessionManagerMatches(manager, tt.tags); got != tt.want {
				t.Fatalf("externalSessionManagerMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasAllocatorSelector(t *testing.T) {
	if hasAllocatorSelector(map[string]string{"repository": "owner/repo"}) {
		t.Fatal("ordinary tags must not be treated as an allocator selector")
	}
	if !hasAllocatorSelector(map[string]string{"allocator.os": "linux"}) {
		t.Fatal("allocator.* tag must be treated as an allocator selector")
	}
}
