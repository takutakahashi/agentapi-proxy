package sessionsettings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSandboxAllowedDomainsAddsLocalRangesToAllowlist(t *testing.T) {
	allowed := []string{"example.com"}

	effective := SandboxAllowedDomains(allowed, nil)

	assert.Equal(t, append([]string{"example.com"}, SandboxLocalAddressRanges...), effective)
	assert.Equal(t, []string{"example.com"}, allowed)
}

func TestSandboxAllowedDomainsAddsLocalRangesToDefaultAllowlist(t *testing.T) {
	assert.Equal(t, SandboxLocalAddressRanges, SandboxAllowedDomains(nil, nil))
}

func TestSandboxAllowedDomainsPreservesDenylistMode(t *testing.T) {
	assert.Empty(t, SandboxAllowedDomains(nil, []string{"example.com"}))
}
