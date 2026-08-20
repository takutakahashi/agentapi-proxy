package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestCachedSessionPreservesStatusMessage(t *testing.T) {
	const message = "provisioner refused connection"
	source := entities.NewProxySessionWithStatus("session-1", "user-1", entities.ScopeUser, "", nil, time.Now(), "error")
	source.SetStatusMessage(message)

	dtos := sessionsToCacheDTOs([]entities.Session{source})
	restored := newCachedSession(dtos[0])

	assert.Equal(t, message, dtos[0].StatusMessage)
	assert.Equal(t, message, restored.StatusMessage())
}
