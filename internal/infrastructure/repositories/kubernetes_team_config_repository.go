package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	// LabelTeamConfig is the label key for team config resources
	LabelTeamConfig = "agentapi.proxy/team-config"
	// LabelTeamID is the label key for team ID
	LabelTeamID = "agentapi.proxy/team-id"
	// SecretKeyConfig is the key in the Secret data for team config JSON
	SecretKeyConfig = "config"
	// TeamConfigSecretPrefix is the prefix for team config Secret names
	TeamConfigSecretPrefix = "agentapi-team-config-"
)

// teamConfigJSON is the JSON representation of team config stored in Secret
type teamConfigJSON struct {
	TeamID         string              `json:"team_id"`
	ServiceAccount *serviceAccountJSON `json:"service_account,omitempty"`
	EnvVars        map[string]string   `json:"env_vars,omitempty"`
}

// serviceAccountJSON is the JSON representation of service account
type serviceAccountJSON struct {
	UserID      string   `json:"user_id"`
	APIKey      string   `json:"api_key"`
	Permissions []string `json:"permissions"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// KubernetesTeamConfigRepository implements TeamConfigRepository using Kubernetes Secrets
type KubernetesTeamConfigRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesTeamConfigRepository creates a new KubernetesTeamConfigRepository
func NewKubernetesTeamConfigRepository(client kubernetes.Interface, namespace string) *KubernetesTeamConfigRepository {
	return &KubernetesTeamConfigRepository{
		client:    client,
		namespace: namespace,
	}
}

// Save persists team configuration (creates or updates)
func (r *KubernetesTeamConfigRepository) Save(ctx context.Context, config *entities.TeamConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid team config: %w", err)
	}

	secretName := r.secretName(config.TeamID())
	labelValue := sanitizeTeamIDForLabel(config.TeamID())

	// Convert to JSON
	data, err := r.toJSON(config)
	if err != nil {
		return fmt.Errorf("failed to marshal team config: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelTeamConfig: "true",
				LabelTeamID:     labelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeyConfig: data,
		},
	}

	// Check if secret exists
	existing, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create team config secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get team config secret: %w", err)
	}

	// Update existing secret
	existing.Data = secret.Data
	existing.Labels = secret.Labels
	_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update team config secret: %w", err)
	}

	return nil
}

// FindByTeamID retrieves a team configuration by team ID
func (r *KubernetesTeamConfigRepository) FindByTeamID(ctx context.Context, teamID string) (*entities.TeamConfig, error) {
	secretName := r.secretName(teamID)

	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("team config not found for team %s", teamID)
		}
		return nil, fmt.Errorf("failed to get team config secret: %w", err)
	}

	config, err := r.fromSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse team config: %w", err)
	}

	return config, nil
}

// Delete removes a team configuration
func (r *KubernetesTeamConfigRepository) Delete(ctx context.Context, teamID string) error {
	secretName := r.secretName(teamID)

	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("team config not found for team %s", teamID)
		}
		return fmt.Errorf("failed to delete team config secret: %w", err)
	}

	return nil
}

// Exists checks if a team configuration exists
func (r *KubernetesTeamConfigRepository) Exists(ctx context.Context, teamID string) (bool, error) {
	secretName := r.secretName(teamID)

	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check team config existence: %w", err)
	}

	return true, nil
}

// List retrieves all team configurations
func (r *KubernetesTeamConfigRepository) List(ctx context.Context) ([]*entities.TeamConfig, error) {
	// List secrets with team config label
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", LabelTeamConfig),
	}

	secretList, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list team config secrets: %w", err)
	}

	configs := make([]*entities.TeamConfig, 0, len(secretList.Items))
	for i := range secretList.Items {
		config, err := r.fromSecret(&secretList.Items[i])
		if err != nil {
			// Log error but continue with other configs
			fmt.Printf("Warning: failed to parse team config from secret %s: %v\n", secretList.Items[i].Name, err)
			continue
		}
		configs = append(configs, config)
	}

	return configs, nil
}

// secretName generates the secret name from team ID
func (r *KubernetesTeamConfigRepository) secretName(teamID string) string {
	// Sanitize team ID for Kubernetes resource name
	sanitized := strings.ToLower(teamID)
	sanitized = strings.ReplaceAll(sanitized, "/", "-")
	// Replace any characters that are not alphanumeric or hyphen
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized = reg.ReplaceAllString(sanitized, "-")
	return TeamConfigSecretPrefix + sanitized
}

// toJSON converts team config to JSON bytes
func (r *KubernetesTeamConfigRepository) toJSON(config *entities.TeamConfig) ([]byte, error) {
	jsonData := &teamConfigJSON{
		TeamID:  config.TeamID(),
		EnvVars: config.EnvVars(),
	}

	// Convert service account if present
	if sa := config.ServiceAccount(); sa != nil {
		permissions := make([]string, len(sa.Permissions()))
		for i, p := range sa.Permissions() {
			permissions[i] = string(p)
		}

		jsonData.ServiceAccount = &serviceAccountJSON{
			UserID:      string(sa.UserID()),
			APIKey:      sa.APIKey(),
			Permissions: permissions,
			CreatedAt:   sa.CreatedAt().Format(time.RFC3339),
			UpdatedAt:   sa.UpdatedAt().Format(time.RFC3339),
		}
	}

	return json.Marshal(jsonData)
}

// fromSecret converts Kubernetes Secret to TeamConfig entity
func (r *KubernetesTeamConfigRepository) fromSecret(secret *corev1.Secret) (*entities.TeamConfig, error) {
	data, ok := secret.Data[SecretKeyConfig]
	if !ok {
		return nil, fmt.Errorf("secret %s does not contain %s key", secret.Name, SecretKeyConfig)
	}

	var jsonData teamConfigJSON
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal team config: %w", err)
	}

	// Convert service account if present
	var serviceAccount *entities.ServiceAccount
	if jsonData.ServiceAccount != nil {
		sa := jsonData.ServiceAccount

		// Parse timestamps
		createdAt, err := time.Parse(time.RFC3339, sa.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
		}

		updatedAt, err := time.Parse(time.RFC3339, sa.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse updated_at: %w", err)
		}

		// Convert permissions
		permissions := make([]entities.Permission, len(sa.Permissions))
		for i, p := range sa.Permissions {
			permissions[i] = entities.Permission(p)
		}

		serviceAccount = entities.NewServiceAccount(
			jsonData.TeamID,
			entities.UserID(sa.UserID),
			sa.APIKey,
			permissions,
		)
		// Set the actual timestamps from storage
		serviceAccount.SetCreatedAt(createdAt)
		serviceAccount.SetUpdatedAt(updatedAt)
	}

	return entities.NewTeamConfig(jsonData.TeamID, serviceAccount, jsonData.EnvVars), nil
}

// sanitizeTeamIDForLabel converts team ID to a valid Kubernetes label value
func sanitizeTeamIDForLabel(teamID string) string {
	// Kubernetes label values must be 63 characters or less
	// and match regex: (([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?
	sanitized := strings.ToLower(teamID)
	sanitized = strings.ReplaceAll(sanitized, "/", "-")
	// Replace any invalid characters
	reg := regexp.MustCompile(`[^a-z0-9-_.]`)
	sanitized = reg.ReplaceAllString(sanitized, "-")

	// Ensure it starts and ends with alphanumeric
	sanitized = strings.Trim(sanitized, "-_.")

	// Truncate to 63 characters
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
		sanitized = strings.TrimRight(sanitized, "-_.")
	}

	return sanitized
}
