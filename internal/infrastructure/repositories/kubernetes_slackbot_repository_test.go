package repositories

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	repoports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesSlackBotRepository_ListEmptyFilterReturnsAllBots(t *testing.T) {
	ctx := context.Background()
	repo := NewKubernetesSlackBotRepository(fake.NewSimpleClientset(), "test-ns")

	userBot := entities.NewSlackBot("user-bot", "User Bot", "user-1")
	require.NoError(t, repo.Create(ctx, userBot))

	teamBot := entities.NewSlackBot("team-bot", "Team Bot", "user-2")
	teamBot.SetScope(entities.ScopeTeam)
	teamBot.SetTeamID("myorg/backend")
	require.NoError(t, repo.Create(ctx, teamBot))

	bots, err := repo.List(ctx, repoports.SlackBotFilter{})
	require.NoError(t, err)
	assert.Len(t, bots, 2)
	assert.ElementsMatch(t, []string{"user-bot", "team-bot"}, slackBotIDs(bots))
}

func TestKubernetesSlackBotRepository_ListUserFilterStillRestrictsTeamBots(t *testing.T) {
	ctx := context.Background()
	repo := NewKubernetesSlackBotRepository(fake.NewSimpleClientset(), "test-ns")

	userBot := entities.NewSlackBot("user-bot", "User Bot", "user-1")
	require.NoError(t, repo.Create(ctx, userBot))

	teamBot := entities.NewSlackBot("team-bot", "Team Bot", "user-2")
	teamBot.SetScope(entities.ScopeTeam)
	teamBot.SetTeamID("myorg/backend")
	require.NoError(t, repo.Create(ctx, teamBot))

	bots, err := repo.List(ctx, repoports.SlackBotFilter{UserID: "user-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"user-bot"}, slackBotIDs(bots))
}

func TestKubernetesSlackBotRepository_PreservesSessionProfileID(t *testing.T) {
	ctx := context.Background()
	repo := NewKubernetesSlackBotRepository(fake.NewSimpleClientset(), "test-ns")

	bot := entities.NewSlackBot("profile-bot", "Profile Bot", "user-1")
	sessionConfig := entities.NewWebhookSessionConfig()
	sessionConfig.SetSessionProfileID("profile-slack")
	bot.SetSessionConfig(sessionConfig)
	require.NoError(t, repo.Create(ctx, bot))

	loaded, err := repo.Get(ctx, "profile-bot")
	require.NoError(t, err)
	require.NotNil(t, loaded.SessionConfig())
	assert.Equal(t, "profile-slack", loaded.SessionConfig().SessionProfileID())
}

func slackBotIDs(bots []*entities.SlackBot) []string {
	ids := make([]string, 0, len(bots))
	for _, bot := range bots {
		ids = append(ids, bot.ID())
	}
	return ids
}
