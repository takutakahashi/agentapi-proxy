package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	repoports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// LabelSlackBot is the label key for slackbot resources
	LabelSlackBot = "agentapi.proxy/slackbot"
	// LabelSlackBotID is the label key for slackbot ID
	LabelSlackBotID = "agentapi.proxy/slackbot-id"
	// LabelSlackBotScope is the label key for slackbot scope (user or team)
	LabelSlackBotScope = "agentapi.proxy/slackbot-scope"
	// LabelSlackBotUserID is the label key for slackbot user ID
	LabelSlackBotUserID = "agentapi.proxy/slackbot-user-id"
	// LabelSlackBotTeamIDHash is the label key for hashed slackbot team ID
	LabelSlackBotTeamIDHash = "agentapi.proxy/slackbot-team-id-hash"
	// AnnotationSlackBotTeamID is the annotation key for original slackbot team ID
	AnnotationSlackBotTeamID = "agentapi.proxy/slackbot-team-id"
	// SecretKeySlackBot is the key in the Secret data for slackbot JSON
	SecretKeySlackBot = "slackbot.json"
	// SlackBotSecretPrefix is the prefix for slackbot Secret names
	SlackBotSecretPrefix = "agentapi-slackbot-"
)

// slackBotJSON is the JSON representation for storage
type slackBotJSON struct {
	ID                 string                    `json:"id"`
	Name               string                    `json:"name"`
	UserID             string                    `json:"user_id"`
	Scope              entities.ResourceScope    `json:"scope,omitempty"`
	TeamID             string                    `json:"team_id,omitempty"`
	Status             entities.SlackBotStatus   `json:"status"`
	SigningSecret      string                    `json:"signing_secret"`
	BotTokenSecretName string                    `json:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey  string                    `json:"bot_token_secret_key,omitempty"`
	AllowedEventTypes  []string                  `json:"allowed_event_types,omitempty"`
	AllowedChannelIDs  []string                  `json:"allowed_channel_ids,omitempty"`
	SessionConfig      *webhookSessionConfigJSON `json:"session_config,omitempty"`
	MaxSessions        int                       `json:"max_sessions,omitempty"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
}

// KubernetesSlackBotRepository implements SlackBotRepository using Kubernetes Secrets
type KubernetesSlackBotRepository struct {
	client    kubernetes.Interface
	namespace string
	mu        sync.RWMutex
}

// NewKubernetesSlackBotRepository creates a new KubernetesSlackBotRepository
func NewKubernetesSlackBotRepository(client kubernetes.Interface, namespace string) *KubernetesSlackBotRepository {
	return &KubernetesSlackBotRepository{
		client:    client,
		namespace: namespace,
	}
}

// slackBotSecretName returns the Secret name for a given slackbot ID
func slackBotSecretName(id string) string {
	return SlackBotSecretPrefix + id
}

// Create creates a new SlackBot
func (r *KubernetesSlackBotRepository) Create(ctx context.Context, slackBot *entities.SlackBot) error {
	if err := slackBot.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if slackbot already exists
	secretName := slackBotSecretName(slackBot.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("slackbot already exists: %s", slackBot.ID())
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check slackbot existence: %w", err)
	}

	// Save slackbot to its own Secret
	if err := r.saveSlackBot(ctx, slackBot); err != nil {
		return fmt.Errorf("failed to save slackbot: %w", err)
	}

	return nil
}

// Get retrieves a SlackBot by ID
func (r *KubernetesSlackBotRepository) Get(ctx context.Context, id string) (*entities.SlackBot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.loadSlackBot(ctx, id)
}

// List retrieves SlackBots matching the filter
func (r *KubernetesSlackBotRepository) List(ctx context.Context, filter repoports.SlackBotFilter) ([]*entities.SlackBot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	slackBots, err := r.loadAllSlackBots(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load slackbots: %w", err)
	}

	var result []*entities.SlackBot
	for _, sb := range slackBots {
		if filter.UserID != "" && sb.UserID() != filter.UserID {
			continue
		}
		if filter.Status != "" && sb.Status() != filter.Status {
			continue
		}
		if filter.Scope != "" && sb.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && sb.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 && sb.Scope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if sb.TeamID() == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}
		result = append(result, sb)
	}

	return result, nil
}

// Update updates an existing SlackBot
func (r *KubernetesSlackBotRepository) Update(ctx context.Context, slackBot *entities.SlackBot) error {
	if err := slackBot.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if slackbot exists
	secretName := slackBotSecretName(slackBot.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return entities.ErrSlackBotNotFound{ID: slackBot.ID()}
		}
		return fmt.Errorf("failed to get slackbot: %w", err)
	}

	slackBot.SetUpdatedAt(time.Now())

	if err := r.saveSlackBot(ctx, slackBot); err != nil {
		return fmt.Errorf("failed to save slackbot: %w", err)
	}

	return nil
}

// Delete removes a SlackBot by ID
func (r *KubernetesSlackBotRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	secretName := slackBotSecretName(id)
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return entities.ErrSlackBotNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete slackbot secret: %w", err)
	}

	return nil
}

// loadSlackBot loads a single slackbot from its Kubernetes Secret
func (r *KubernetesSlackBotRepository) loadSlackBot(ctx context.Context, id string) (*entities.SlackBot, error) {
	secretName := slackBotSecretName(id)
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, entities.ErrSlackBotNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get slackbot secret: %w", err)
	}

	data, ok := secret.Data[SecretKeySlackBot]
	if !ok {
		return nil, fmt.Errorf("slackbot secret missing data key: %s", SecretKeySlackBot)
	}

	var sbj slackBotJSON
	if err := json.Unmarshal(data, &sbj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal slackbot: %w", err)
	}

	return r.jsonToEntity(&sbj), nil
}

// loadAllSlackBots loads all slackbots from Kubernetes Secrets
func (r *KubernetesSlackBotRepository) loadAllSlackBots(ctx context.Context) ([]*entities.SlackBot, error) {
	labelSelector := fmt.Sprintf("%s=true", LabelSlackBot)
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list slackbot secrets: %w", err)
	}

	result := make([]*entities.SlackBot, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		data, ok := secret.Data[SecretKeySlackBot]
		if !ok {
			continue
		}

		var sbj slackBotJSON
		if err := json.Unmarshal(data, &sbj); err != nil {
			continue
		}

		// Prefer team_id from annotation over JSON data
		// Use the annotation value as-is (should be in slash format: org/team-slug)
		if annotationTeamID, ok := secret.Annotations[AnnotationSlackBotTeamID]; ok && annotationTeamID != "" {
			sbj.TeamID = annotationTeamID
		}

		slackBot := r.jsonToEntity(&sbj)
		result = append(result, slackBot)
	}

	return result, nil
}

// saveSlackBot saves a slackbot to its own Kubernetes Secret
func (r *KubernetesSlackBotRepository) saveSlackBot(ctx context.Context, slackBot *entities.SlackBot) error {
	sbj := r.entityToJSON(slackBot)
	data, err := json.Marshal(sbj)
	if err != nil {
		return fmt.Errorf("failed to marshal slackbot: %w", err)
	}

	secretName := slackBotSecretName(slackBot.ID())
	labels := map[string]string{
		LabelSlackBot:       "true",
		LabelSlackBotID:     slackBot.ID(),
		LabelSlackBotScope:  string(slackBot.Scope()),
		LabelSlackBotUserID: slackBot.UserID(),
	}
	annotations := make(map[string]string)
	if slackBot.TeamID() != "" {
		// Use sha256 hash for team-id label to avoid issues with "/" in team IDs
		// The original team_id is stored in annotations for restoration
		labels[LabelSlackBotTeamIDHash] = services.HashTeamID(slackBot.TeamID())
		annotations[AnnotationSlackBotTeamID] = slackBot.TeamID()
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   r.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeySlackBot: data,
		},
	}

	// Try to create first
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existing, getErr := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}

			existing.Data[SecretKeySlackBot] = data
			// Ensure labels and annotations are set
			existing.Labels = labels
			existing.Annotations = annotations

			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update slackbot secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create slackbot secret: %w", err)
	}

	return nil
}

// jsonToEntity converts JSON representation to entity
func (r *KubernetesSlackBotRepository) jsonToEntity(sbj *slackBotJSON) *entities.SlackBot {
	slackBot := entities.NewSlackBot(sbj.ID, sbj.Name, sbj.UserID)
	slackBot.SetScope(sbj.Scope)
	slackBot.SetTeamID(sbj.TeamID)
	slackBot.SetStatus(sbj.Status)
	slackBot.SetSigningSecret(sbj.SigningSecret)
	if sbj.BotTokenSecretName != "" {
		slackBot.SetBotTokenSecretName(sbj.BotTokenSecretName)
	}
	if sbj.BotTokenSecretKey != "" {
		slackBot.SetBotTokenSecretKey(sbj.BotTokenSecretKey)
	}
	if len(sbj.AllowedEventTypes) > 0 {
		slackBot.SetAllowedEventTypes(sbj.AllowedEventTypes)
	}
	if len(sbj.AllowedChannelIDs) > 0 {
		slackBot.SetAllowedChannelIDs(sbj.AllowedChannelIDs)
	}
	if sbj.MaxSessions > 0 {
		slackBot.SetMaxSessions(sbj.MaxSessions)
	}
	if sbj.SessionConfig != nil {
		slackBot.SetSessionConfig(r.sessionConfigJSONToSlackBotEntity(sbj.SessionConfig))
	}
	slackBot.SetCreatedAt(sbj.CreatedAt)
	slackBot.SetUpdatedAt(sbj.UpdatedAt)
	return slackBot
}

// entityToJSON converts entity to JSON representation
func (r *KubernetesSlackBotRepository) entityToJSON(sb *entities.SlackBot) *slackBotJSON {
	sbj := &slackBotJSON{
		ID:                 sb.ID(),
		Name:               sb.Name(),
		UserID:             sb.UserID(),
		Scope:              sb.Scope(),
		TeamID:             sb.TeamID(),
		Status:             sb.Status(),
		SigningSecret:      sb.SigningSecret(),
		BotTokenSecretName: sb.BotTokenSecretName(),
		BotTokenSecretKey:  sb.BotTokenSecretKey(),
		AllowedEventTypes:  sb.AllowedEventTypes(),
		AllowedChannelIDs:  sb.AllowedChannelIDs(),
		MaxSessions:        sb.MaxSessions(),
		CreatedAt:          sb.CreatedAt(),
		UpdatedAt:          sb.UpdatedAt(),
	}

	if sc := sb.SessionConfig(); sc != nil {
		sbj.SessionConfig = r.sessionConfigSlackBotEntityToJSON(sc)
	}

	return sbj
}

// sessionConfigJSONToSlackBotEntity converts session config JSON to entity
// Reuses webhookSessionConfigJSON since SlackBot uses the same WebhookSessionConfig type
func (r *KubernetesSlackBotRepository) sessionConfigJSONToSlackBotEntity(scj *webhookSessionConfigJSON) *entities.WebhookSessionConfig {
	sc := entities.NewWebhookSessionConfig()
	sc.SetEnvironment(scj.Environment)
	sc.SetTags(scj.Tags)
	sc.SetInitialMessageTemplate(scj.InitialMessageTemplate)
	sc.SetReuseMessageTemplate(scj.ReuseMessageTemplate)
	sc.SetReuseSession(scj.ReuseSession)
	sc.SetMountPayload(scj.MountPayload)
	if scj.Params != nil {
		sc.SetParams(scj.Params)
	}
	return sc
}

// sessionConfigSlackBotEntityToJSON converts session config entity to JSON
// Reuses webhookSessionConfigJSON since SlackBot uses the same WebhookSessionConfig type
func (r *KubernetesSlackBotRepository) sessionConfigSlackBotEntityToJSON(sc *entities.WebhookSessionConfig) *webhookSessionConfigJSON {
	scj := &webhookSessionConfigJSON{
		Environment:            sc.Environment(),
		Tags:                   sc.Tags(),
		InitialMessageTemplate: sc.InitialMessageTemplate(),
		ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
		ReuseSession:           sc.ReuseSession(),
		MountPayload:           sc.MountPayload(),
	}
	if params := sc.Params(); params != nil {
		scj.Params = params
	}
	return scj
}
