package cmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestConfigureWorkerSlackCredential(t *testing.T) {
	store, err := kvstore.NewLibSQLStore(context.Background(), "file://"+filepath.Join(t.TempDir(), "worker.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	client := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	cfg := &config.Config{Slack: config.SlackConfig{AppToken: "xapp-test", BotToken: "xoxb-test"}}

	require.NoError(t, configureWorkerSlackCredential(context.Background(), cfg, client, "test"))
	secret, err := client.CoreV1().Secrets("test").Get(context.Background(), workerDefaultSlackSecretName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "xapp-test", string(secret.Data["app-token"]))
	require.Equal(t, "xoxb-test", string(secret.Data["bot-token"]))
	require.Equal(t, workerDefaultSlackSecretName, cfg.Slack.AppTokenSecretName)
	require.Equal(t, workerDefaultSlackSecretName, cfg.KubernetesSession.SlackBotTokenSecretName)
}

func TestConfigureWorkerSlackCredentialRequiresPair(t *testing.T) {
	store, err := kvstore.NewLibSQLStore(context.Background(), "file://"+filepath.Join(t.TempDir(), "worker.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	client := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	cfg := &config.Config{Slack: config.SlackConfig{AppToken: "xapp-test"}}

	require.ErrorContains(t, configureWorkerSlackCredential(context.Background(), cfg, client, "test"), "requires both")
}
