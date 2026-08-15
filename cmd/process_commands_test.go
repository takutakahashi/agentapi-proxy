package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSeparatedProcessCommands(t *testing.T) {
	assert.Equal(t, "worker", WorkerCmd.Use)
	assert.Equal(t, "session-manager", SessionManagerCmd.Use)
	assert.NotNil(t, WorkerCmd.RunE)
	assert.NotNil(t, SessionManagerCmd.RunE)
}
