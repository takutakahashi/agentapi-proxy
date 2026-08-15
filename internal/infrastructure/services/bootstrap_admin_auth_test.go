package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestLoadBootstrapAdmin(t *testing.T) {
	service := NewSimpleAuthService()
	require.NoError(t, service.LoadBootstrapAdmin("root-admin", "Root Admin", "break-glass-token"))

	user, err := service.ValidateAPIKey(context.Background(), "break-glass-token")
	require.NoError(t, err)
	require.Equal(t, entities.UserID("root-admin"), user.ID())
	require.Equal(t, "Root Admin", user.Username())
	require.True(t, user.IsAdmin())
	require.NoError(t, service.ValidatePermission(context.Background(), user, entities.PermissionAdmin))
}

func TestLoadBootstrapAdminRejectsMissingFields(t *testing.T) {
	service := NewSimpleAuthService()
	require.Error(t, service.LoadBootstrapAdmin("", "admin", "token"))
	require.Error(t, service.LoadBootstrapAdmin("admin", "", "token"))
	require.Error(t, service.LoadBootstrapAdmin("admin", "admin", ""))
}
