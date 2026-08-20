package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigBootstrapAdminFromEnvironment(t *testing.T) {
	t.Setenv("AGENTAPI_AUTH_BOOTSTRAP_ADMIN_ENABLED", "true")
	t.Setenv("AGENTAPI_AUTH_BOOTSTRAP_ADMIN_USER_ID", "helm-admin")
	t.Setenv("AGENTAPI_AUTH_BOOTSTRAP_ADMIN_USERNAME", "Helm Admin")
	t.Setenv("AGENTAPI_AUTH_BOOTSTRAP_ADMIN_TOKEN", "secret-token")

	loaded, err := LoadConfig("")
	require.NoError(t, err)
	require.NotNil(t, loaded.Auth.BootstrapAdmin)
	require.True(t, loaded.Auth.BootstrapAdmin.Enabled)
	require.Equal(t, "helm-admin", loaded.Auth.BootstrapAdmin.UserID)
	require.Equal(t, "Helm Admin", loaded.Auth.BootstrapAdmin.Username)
	require.Equal(t, "secret-token", loaded.Auth.BootstrapAdmin.Token)
}
