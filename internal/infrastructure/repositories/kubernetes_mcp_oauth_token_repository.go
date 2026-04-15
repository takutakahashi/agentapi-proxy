package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	// MCPOAuthSecretPrefix is the Secret name prefix for MCP OAuth tokens.
	MCPOAuthSecretPrefix = "agentapi-mcp-oauth-"
	// LabelMCPOAuth is the label applied to all MCP OAuth Secrets.
	LabelMCPOAuth = "agentapi.proxy/mcp-oauth"
	// SecretKeyMCPOAuth is the data key inside the Secret.
	SecretKeyMCPOAuth = "tokens.json"
)

// mcpOAuthTokensJSON is the on-disk representation stored in the Kubernetes Secret.
// It is a map of serverName → token record.
type mcpOAuthTokensJSON map[string]*mcpOAuthTokenRecord

type mcpOAuthTokenRecord struct {
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
}

// KubernetesMCPOAuthTokenRepository implements MCPOAuthTokenRepository using
// one Kubernetes Secret per user (agentapi-mcp-oauth-{sanitizedUserID}).
type KubernetesMCPOAuthTokenRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesMCPOAuthTokenRepository creates a new repository.
func NewKubernetesMCPOAuthTokenRepository(client kubernetes.Interface, namespace string) *KubernetesMCPOAuthTokenRepository {
	return &KubernetesMCPOAuthTokenRepository{
		client:    client,
		namespace: namespace,
	}
}

// Save persists the token for the given user × server, creating the Secret if necessary.
func (r *KubernetesMCPOAuthTokenRepository) Save(ctx context.Context, token *entities.MCPOAuthToken) error {
	secretName := r.secretName(token.UserID())

	tokens, secret, err := r.loadSecret(ctx, secretName)
	if err != nil {
		return err
	}

	tokens[token.ServerName()] = &mcpOAuthTokenRecord{
		ClientID:     token.ClientID(),
		ClientSecret: token.ClientSecret(),
		AccessToken:  token.AccessToken(),
		RefreshToken: token.RefreshToken(),
		ExpiresAt:    token.ExpiresAt(),
		TokenType:    token.TokenType(),
		TokenURL:     token.TokenURL(),
	}

	data, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("mcp oauth repo: marshal: %w", err)
	}

	if secret == nil {
		// Create new Secret.
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: r.namespace,
				Labels: map[string]string{
					LabelMCPOAuth: "true",
				},
			},
			Data: map[string][]byte{
				SecretKeyMCPOAuth: data,
			},
		}
		_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, newSecret, metav1.CreateOptions{})
		return err
	}

	// Update existing Secret.
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[SecretKeyMCPOAuth] = data
	_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

// FindByUserAndServer returns the stored token or nil when not found.
func (r *KubernetesMCPOAuthTokenRepository) FindByUserAndServer(ctx context.Context, userID, serverName string) (*entities.MCPOAuthToken, error) {
	tokens, _, err := r.loadSecret(ctx, r.secretName(userID))
	if err != nil {
		return nil, err
	}
	rec, ok := tokens[serverName]
	if !ok {
		return nil, nil
	}
	return r.recordToEntity(userID, serverName, rec), nil
}

// Delete removes a single server's token from the Secret.
func (r *KubernetesMCPOAuthTokenRepository) Delete(ctx context.Context, userID, serverName string) error {
	secretName := r.secretName(userID)
	tokens, secret, err := r.loadSecret(ctx, secretName)
	if err != nil {
		return err
	}
	if secret == nil {
		return nil
	}
	delete(tokens, serverName)

	data, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	secret.Data[SecretKeyMCPOAuth] = data
	_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

// ListByUser returns all tokens for a user.
func (r *KubernetesMCPOAuthTokenRepository) ListByUser(ctx context.Context, userID string) ([]*entities.MCPOAuthToken, error) {
	tokens, _, err := r.loadSecret(ctx, r.secretName(userID))
	if err != nil {
		return nil, err
	}
	result := make([]*entities.MCPOAuthToken, 0, len(tokens))
	for serverName, rec := range tokens {
		result = append(result, r.recordToEntity(userID, serverName, rec))
	}
	return result, nil
}

// ---- helpers ----

func (r *KubernetesMCPOAuthTokenRepository) secretName(userID string) string {
	return MCPOAuthSecretPrefix + sanitizeName(userID)
}

// loadSecret fetches the Secret and deserialises the token map.
// Returns an empty map and nil secret when the Secret does not exist yet.
func (r *KubernetesMCPOAuthTokenRepository) loadSecret(ctx context.Context, secretName string) (mcpOAuthTokensJSON, *corev1.Secret, error) {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return make(mcpOAuthTokensJSON), nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("mcp oauth repo: get secret %s: %w", secretName, err)
	}

	var tokens mcpOAuthTokensJSON
	if raw, ok := secret.Data[SecretKeyMCPOAuth]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &tokens); err != nil {
			return nil, nil, fmt.Errorf("mcp oauth repo: unmarshal: %w", err)
		}
	}
	if tokens == nil {
		tokens = make(mcpOAuthTokensJSON)
	}
	return tokens, secret, nil
}

func (r *KubernetesMCPOAuthTokenRepository) recordToEntity(userID, serverName string, rec *mcpOAuthTokenRecord) *entities.MCPOAuthToken {
	t := entities.NewMCPOAuthToken(userID, serverName)
	t.SetClientID(rec.ClientID)
	t.SetClientSecret(rec.ClientSecret)
	t.SetAccessToken(rec.AccessToken)
	t.SetRefreshToken(rec.RefreshToken)
	t.SetExpiresAt(rec.ExpiresAt)
	t.SetTokenType(rec.TokenType)
	t.SetTokenURL(rec.TokenURL)
	return t
}

// sanitizeName converts a name to a valid Kubernetes label/resource suffix.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	s := b.String()
	// Trim leading/trailing dashes.
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}
