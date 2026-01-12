package repositories

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// LabelWebhook is the label key for webhook resources
	LabelWebhook = "agentapi.proxy/webhook"
	// LabelWebhookID is the label key for webhook ID
	LabelWebhookID = "agentapi.proxy/webhook-id"
	// LabelWebhookScope is the label key for webhook scope (user or team)
	LabelWebhookScope = "agentapi.proxy/webhook-scope"
	// LabelWebhookUserID is the label key for webhook user ID
	LabelWebhookUserID = "agentapi.proxy/webhook-user-id"
	// LabelWebhookTeamID is the label key for webhook team ID
	LabelWebhookTeamID = "agentapi.proxy/webhook-team-id"
	// SecretKeyWebhook is the key in the Secret data for webhook JSON
	SecretKeyWebhook = "webhook.json"
	// WebhookSecretPrefix is the prefix for webhook Secret names
	WebhookSecretPrefix = "agentapi-webhook-"
)

// webhookJSON is the JSON representation for storage
type webhookJSON struct {
	ID              string                        `json:"id"`
	Name            string                        `json:"name"`
	UserID          string                        `json:"user_id"`
	Scope           entities.ResourceScope        `json:"scope,omitempty"`
	TeamID          string                        `json:"team_id,omitempty"`
	Status          entities.WebhookStatus        `json:"status"`
	Type            entities.WebhookType          `json:"type"`
	Secret          string                        `json:"secret"`
	SignatureHeader string                        `json:"signature_header,omitempty"`
	SignatureType   entities.WebhookSignatureType `json:"signature_type,omitempty"`
	GitHub          *webhookGitHubConfigJSON      `json:"github,omitempty"`
	Triggers        []webhookTriggerJSON          `json:"triggers"`
	SessionConfig   *webhookSessionConfigJSON     `json:"session_config,omitempty"`
	CreatedAt       time.Time                     `json:"created_at"`
	UpdatedAt       time.Time                     `json:"updated_at"`
	LastDelivery    *webhookDeliveryRecordJSON    `json:"last_delivery,omitempty"`
	DeliveryCount   int64                         `json:"delivery_count"`
}

type webhookGitHubConfigJSON struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

type webhookTriggerJSON struct {
	ID            string                       `json:"id"`
	Name          string                       `json:"name"`
	Priority      int                          `json:"priority"`
	Enabled       bool                         `json:"enabled"`
	Conditions    webhookTriggerConditionsJSON `json:"conditions"`
	SessionConfig *webhookSessionConfigJSON    `json:"session_config,omitempty"`
	StopOnMatch   bool                         `json:"stop_on_match"`
}

type webhookTriggerConditionsJSON struct {
	GitHub   *webhookGitHubConditionsJSON   `json:"github,omitempty"`
	JSONPath []webhookJSONPathConditionJSON `json:"jsonpath,omitempty"`
}

type webhookJSONPathConditionJSON struct {
	Path     string      `json:"path"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

type webhookGitHubConditionsJSON struct {
	Events       []string `json:"events,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	BaseBranches []string `json:"base_branches,omitempty"`
	Draft        *bool    `json:"draft,omitempty"`
	Sender       []string `json:"sender,omitempty"`
}

type webhookSessionConfigJSON struct {
	Environment            map[string]string         `json:"environment,omitempty"`
	Tags                   map[string]string         `json:"tags,omitempty"`
	InitialMessageTemplate string                    `json:"initial_message_template,omitempty"`
	Params                 *webhookSessionParamsJSON `json:"params,omitempty"`
}

type webhookSessionParamsJSON struct {
	GithubToken string `json:"github_token,omitempty"`
}

type webhookDeliveryRecordJSON struct {
	ID             string                  `json:"id"`
	ReceivedAt     time.Time               `json:"received_at"`
	Status         entities.DeliveryStatus `json:"status"`
	MatchedTrigger string                  `json:"matched_trigger,omitempty"`
	SessionID      string                  `json:"session_id,omitempty"`
	Error          string                  `json:"error,omitempty"`
}

// KubernetesWebhookRepository implements WebhookRepository using Kubernetes Secrets
type KubernetesWebhookRepository struct {
	client                      kubernetes.Interface
	namespace                   string
	defaultGitHubEnterpriseHost string
	mu                          sync.RWMutex
}

// NewKubernetesWebhookRepository creates a new KubernetesWebhookRepository
func NewKubernetesWebhookRepository(client kubernetes.Interface, namespace string) *KubernetesWebhookRepository {
	return &KubernetesWebhookRepository{
		client:    client,
		namespace: namespace,
	}
}

// SetDefaultGitHubEnterpriseHost sets the default GitHub Enterprise host for webhook matching
// When set, webhooks without explicit enterprise_url will match against this host
func (r *KubernetesWebhookRepository) SetDefaultGitHubEnterpriseHost(host string) {
	r.defaultGitHubEnterpriseHost = host
}

// webhookSecretName returns the Secret name for a given webhook ID
func webhookSecretName(id string) string {
	return WebhookSecretPrefix + id
}

// Create creates a new webhook
func (r *KubernetesWebhookRepository) Create(ctx context.Context, webhook *entities.Webhook) error {
	if err := webhook.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if webhook already exists
	secretName := webhookSecretName(webhook.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("webhook already exists: %s", webhook.ID())
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check webhook existence: %w", err)
	}

	// Generate secret if not provided
	if webhook.Secret() == "" {
		secret, err := generateWebhookSecret(32)
		if err != nil {
			return fmt.Errorf("failed to generate secret: %w", err)
		}
		webhook.SetSecret(secret)
	}

	// Save webhook to its own Secret
	if err := r.saveWebhook(ctx, webhook); err != nil {
		return fmt.Errorf("failed to save webhook: %w", err)
	}

	return nil
}

// Get retrieves a webhook by ID
func (r *KubernetesWebhookRepository) Get(ctx context.Context, id string) (*entities.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.loadWebhook(ctx, id)
}

// List retrieves webhooks matching the filter
func (r *KubernetesWebhookRepository) List(ctx context.Context, filter repositories.WebhookFilter) ([]*entities.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	webhooks, err := r.loadAllWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load webhooks: %w", err)
	}

	var result []*entities.Webhook
	for _, w := range webhooks {
		if filter.UserID != "" && w.UserID() != filter.UserID {
			continue
		}
		if filter.Status != "" && w.Status() != filter.Status {
			continue
		}
		if filter.Type != "" && w.WebhookType() != filter.Type {
			continue
		}
		if filter.Scope != "" && w.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && w.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 && w.Scope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if w.TeamID() == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}
		result = append(result, w)
	}

	return result, nil
}

// Update updates an existing webhook
func (r *KubernetesWebhookRepository) Update(ctx context.Context, webhook *entities.Webhook) error {
	if err := webhook.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if webhook exists
	secretName := webhookSecretName(webhook.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return entities.ErrWebhookNotFound{ID: webhook.ID()}
		}
		return fmt.Errorf("failed to get webhook: %w", err)
	}

	webhook.SetUpdatedAt(time.Now())

	if err := r.saveWebhook(ctx, webhook); err != nil {
		return fmt.Errorf("failed to save webhook: %w", err)
	}

	return nil
}

// Delete removes a webhook by ID
func (r *KubernetesWebhookRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	secretName := webhookSecretName(id)
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return entities.ErrWebhookNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete webhook secret: %w", err)
	}

	return nil
}

// FindByGitHubRepository finds webhooks that may match a GitHub webhook
func (r *KubernetesWebhookRepository) FindByGitHubRepository(ctx context.Context, matcher repositories.GitHubMatcher) ([]*entities.Webhook, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	webhooks, err := r.loadAllWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load webhooks: %w", err)
	}

	var result []*entities.Webhook
	for _, w := range webhooks {
		// Only GitHub webhooks
		if w.WebhookType() != entities.WebhookTypeGitHub {
			continue
		}

		// Only active webhooks
		if w.Status() != entities.WebhookStatusActive {
			continue
		}

		github := w.GitHub()
		if github != nil {
			// Check enterprise URL match
			// Use webhook's explicit enterprise_url if set, otherwise use the default from config
			webhookEnterpriseURL := github.EnterpriseURL()
			if webhookEnterpriseURL == "" && r.defaultGitHubEnterpriseHost != "" {
				webhookEnterpriseURL = r.defaultGitHubEnterpriseHost
			}
			normalizedWebhookURL := normalizeEnterpriseURL(webhookEnterpriseURL)
			normalizedMatcherURL := normalizeEnterpriseURL(matcher.EnterpriseURL)
			if normalizedWebhookURL != normalizedMatcherURL {
				continue
			}

			// Check allowed events
			if len(github.AllowedEvents()) > 0 {
				eventAllowed := false
				for _, allowedEvent := range github.AllowedEvents() {
					if allowedEvent == matcher.Event {
						eventAllowed = true
						break
					}
				}
				if !eventAllowed {
					continue
				}
			}

			// Check allowed repositories
			if len(github.AllowedRepositories()) > 0 {
				repoAllowed := false
				for _, allowedRepo := range github.AllowedRepositories() {
					if matchWebhookRepository(allowedRepo, matcher.Repository) {
						repoAllowed = true
						break
					}
				}
				if !repoAllowed {
					continue
				}
			}
		}

		result = append(result, w)
	}

	return result, nil
}

// RecordDelivery records a webhook delivery
func (r *KubernetesWebhookRepository) RecordDelivery(ctx context.Context, id string, record *entities.WebhookDeliveryRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	webhook, err := r.loadWebhook(ctx, id)
	if err != nil {
		return err
	}

	webhook.SetLastDelivery(record)
	webhook.IncrementDeliveryCount()

	if err := r.saveWebhook(ctx, webhook); err != nil {
		return fmt.Errorf("failed to save webhook: %w", err)
	}

	return nil
}

// loadWebhook loads a single webhook from its Kubernetes Secret
func (r *KubernetesWebhookRepository) loadWebhook(ctx context.Context, id string) (*entities.Webhook, error) {
	secretName := webhookSecretName(id)
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, entities.ErrWebhookNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get webhook secret: %w", err)
	}

	data, ok := secret.Data[SecretKeyWebhook]
	if !ok {
		return nil, fmt.Errorf("webhook secret missing data key: %s", SecretKeyWebhook)
	}

	var wj webhookJSON
	if err := json.Unmarshal(data, &wj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal webhook: %w", err)
	}

	return r.jsonToEntity(&wj), nil
}

// loadAllWebhooks loads all webhooks from Kubernetes Secrets
func (r *KubernetesWebhookRepository) loadAllWebhooks(ctx context.Context) ([]*entities.Webhook, error) {
	labelSelector := fmt.Sprintf("%s=true", LabelWebhook)
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list webhook secrets: %w", err)
	}

	result := make([]*entities.Webhook, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		data, ok := secret.Data[SecretKeyWebhook]
		if !ok {
			continue
		}

		var wj webhookJSON
		if err := json.Unmarshal(data, &wj); err != nil {
			continue
		}

		webhook := r.jsonToEntity(&wj)
		result = append(result, webhook)
	}

	return result, nil
}

// saveWebhook saves a webhook to its own Kubernetes Secret
func (r *KubernetesWebhookRepository) saveWebhook(ctx context.Context, webhook *entities.Webhook) error {
	wj := r.entityToJSON(webhook)
	data, err := json.Marshal(wj)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook: %w", err)
	}

	secretName := webhookSecretName(webhook.ID())
	labels := map[string]string{
		LabelWebhook:       "true",
		LabelWebhookID:     webhook.ID(),
		LabelWebhookScope:  string(webhook.Scope()),
		LabelWebhookUserID: webhook.UserID(),
	}
	if webhook.TeamID() != "" {
		labels[LabelWebhookTeamID] = webhook.TeamID()
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeyWebhook: data,
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

			existing.Data[SecretKeyWebhook] = data
			// Ensure labels are set
			existing.Labels = labels

			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update webhook secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create webhook secret: %w", err)
	}

	return nil
}

// jsonToEntity converts JSON representation to entity
func (r *KubernetesWebhookRepository) jsonToEntity(wj *webhookJSON) *entities.Webhook {
	webhook := entities.NewWebhook(wj.ID, wj.Name, wj.UserID, wj.Type)
	webhook.SetScope(wj.Scope)
	webhook.SetTeamID(wj.TeamID)
	webhook.SetStatus(wj.Status)
	webhook.SetSecret(wj.Secret)
	if wj.SignatureHeader != "" {
		webhook.SetSignatureHeader(wj.SignatureHeader)
	}
	if wj.SignatureType != "" {
		webhook.SetSignatureType(wj.SignatureType)
	}

	// GitHub config
	if wj.GitHub != nil {
		github := entities.NewWebhookGitHubConfig()
		github.SetEnterpriseURL(wj.GitHub.EnterpriseURL)
		github.SetAllowedEvents(wj.GitHub.AllowedEvents)
		github.SetAllowedRepositories(wj.GitHub.AllowedRepositories)
		webhook.SetGitHub(github)
	}

	// Triggers
	triggers := make([]entities.WebhookTrigger, 0, len(wj.Triggers))
	for _, tj := range wj.Triggers {
		trigger := entities.NewWebhookTrigger(tj.ID, tj.Name)
		trigger.SetPriority(tj.Priority)
		trigger.SetEnabled(tj.Enabled)
		trigger.SetStopOnMatch(tj.StopOnMatch)

		// Conditions
		var conditions entities.WebhookTriggerConditions
		if tj.Conditions.GitHub != nil {
			ghCond := entities.NewWebhookGitHubConditions()
			ghCond.SetEvents(tj.Conditions.GitHub.Events)
			ghCond.SetActions(tj.Conditions.GitHub.Actions)
			ghCond.SetBranches(tj.Conditions.GitHub.Branches)
			ghCond.SetRepositories(tj.Conditions.GitHub.Repositories)
			ghCond.SetLabels(tj.Conditions.GitHub.Labels)
			ghCond.SetPaths(tj.Conditions.GitHub.Paths)
			ghCond.SetBaseBranches(tj.Conditions.GitHub.BaseBranches)
			ghCond.SetDraft(tj.Conditions.GitHub.Draft)
			ghCond.SetSender(tj.Conditions.GitHub.Sender)
			conditions.SetGitHub(ghCond)
		}
		if len(tj.Conditions.JSONPath) > 0 {
			jsonPathConditions := make([]entities.WebhookJSONPathCondition, 0, len(tj.Conditions.JSONPath))
			for _, jp := range tj.Conditions.JSONPath {
				jsonPathConditions = append(jsonPathConditions, entities.NewWebhookJSONPathCondition(
					jp.Path,
					jp.Operator,
					jp.Value,
				))
			}
			conditions.SetJSONPath(jsonPathConditions)
		}
		trigger.SetConditions(conditions)

		// Session config
		if tj.SessionConfig != nil {
			trigger.SetSessionConfig(r.sessionConfigJSONToEntity(tj.SessionConfig))
		}

		triggers = append(triggers, trigger)
	}
	webhook.SetTriggers(triggers)

	// Session config
	if wj.SessionConfig != nil {
		webhook.SetSessionConfig(r.sessionConfigJSONToEntity(wj.SessionConfig))
	}

	// Last delivery
	if wj.LastDelivery != nil {
		record := entities.NewWebhookDeliveryRecord(wj.LastDelivery.ID, wj.LastDelivery.Status)
		record.SetMatchedTrigger(wj.LastDelivery.MatchedTrigger)
		record.SetSessionID(wj.LastDelivery.SessionID)
		record.SetError(wj.LastDelivery.Error)
		webhook.SetLastDelivery(record)
	}

	return webhook
}

// entityToJSON converts entity to JSON representation
func (r *KubernetesWebhookRepository) entityToJSON(w *entities.Webhook) *webhookJSON {
	wj := &webhookJSON{
		ID:              w.ID(),
		Name:            w.Name(),
		UserID:          w.UserID(),
		Scope:           w.Scope(),
		TeamID:          w.TeamID(),
		Status:          w.Status(),
		Type:            w.WebhookType(),
		Secret:          w.Secret(),
		SignatureHeader: w.SignatureHeader(),
		SignatureType:   w.SignatureType(),
		CreatedAt:       w.CreatedAt(),
		UpdatedAt:       w.UpdatedAt(),
		DeliveryCount:   w.DeliveryCount(),
	}

	// GitHub config
	if gh := w.GitHub(); gh != nil {
		wj.GitHub = &webhookGitHubConfigJSON{
			EnterpriseURL:       gh.EnterpriseURL(),
			AllowedEvents:       gh.AllowedEvents(),
			AllowedRepositories: gh.AllowedRepositories(),
		}
	}

	// Triggers
	triggers := w.Triggers()
	wj.Triggers = make([]webhookTriggerJSON, 0, len(triggers))
	for _, t := range triggers {
		tj := webhookTriggerJSON{
			ID:          t.ID(),
			Name:        t.Name(),
			Priority:    t.Priority(),
			Enabled:     t.Enabled(),
			StopOnMatch: t.StopOnMatch(),
		}

		// Conditions
		cond := t.Conditions()
		if ghCond := cond.GitHub(); ghCond != nil {
			tj.Conditions.GitHub = &webhookGitHubConditionsJSON{
				Events:       ghCond.Events(),
				Actions:      ghCond.Actions(),
				Branches:     ghCond.Branches(),
				Repositories: ghCond.Repositories(),
				Labels:       ghCond.Labels(),
				Paths:        ghCond.Paths(),
				BaseBranches: ghCond.BaseBranches(),
				Draft:        ghCond.Draft(),
				Sender:       ghCond.Sender(),
			}
		}
		if jsonPathConds := cond.JSONPath(); len(jsonPathConds) > 0 {
			tj.Conditions.JSONPath = make([]webhookJSONPathConditionJSON, 0, len(jsonPathConds))
			for _, jp := range jsonPathConds {
				tj.Conditions.JSONPath = append(tj.Conditions.JSONPath, webhookJSONPathConditionJSON{
					Path:     jp.Path(),
					Operator: string(jp.Operator()),
					Value:    jp.Value(),
				})
			}
		}

		// Session config
		if sc := t.SessionConfig(); sc != nil {
			tj.SessionConfig = r.sessionConfigEntityToJSON(sc)
		}

		wj.Triggers = append(wj.Triggers, tj)
	}

	// Session config
	if sc := w.SessionConfig(); sc != nil {
		wj.SessionConfig = r.sessionConfigEntityToJSON(sc)
	}

	// Last delivery
	if ld := w.LastDelivery(); ld != nil {
		wj.LastDelivery = &webhookDeliveryRecordJSON{
			ID:             ld.ID(),
			ReceivedAt:     ld.ReceivedAt(),
			Status:         ld.Status(),
			MatchedTrigger: ld.MatchedTrigger(),
			SessionID:      ld.SessionID(),
			Error:          ld.Error(),
		}
	}

	return wj
}

func (r *KubernetesWebhookRepository) sessionConfigJSONToEntity(scj *webhookSessionConfigJSON) *entities.WebhookSessionConfig {
	sc := entities.NewWebhookSessionConfig()
	sc.SetEnvironment(scj.Environment)
	sc.SetTags(scj.Tags)
	sc.SetInitialMessageTemplate(scj.InitialMessageTemplate)
	if scj.Params != nil {
		params := entities.NewWebhookSessionParams()
		params.SetGithubToken(scj.Params.GithubToken)
		sc.SetParams(params)
	}
	return sc
}

func (r *KubernetesWebhookRepository) sessionConfigEntityToJSON(sc *entities.WebhookSessionConfig) *webhookSessionConfigJSON {
	scj := &webhookSessionConfigJSON{
		Environment:            sc.Environment(),
		Tags:                   sc.Tags(),
		InitialMessageTemplate: sc.InitialMessageTemplate(),
	}
	if params := sc.Params(); params != nil {
		scj.Params = &webhookSessionParamsJSON{
			GithubToken: params.GithubToken(),
		}
	}
	return scj
}

// Helper functions

func generateWebhookSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func normalizeEnterpriseURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	url = strings.ToLower(url)
	// Remove URL scheme (https:// or http://) to match GitHub Enterprise Host header
	// GitHub sends only the hostname in X-GitHub-Enterprise-Host header
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	return url
}

func matchWebhookRepository(pattern, repository string) bool {
	if pattern == repository {
		return true
	}

	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		parts := strings.SplitN(repository, "/", 2)
		if len(parts) == 2 && parts[0] == prefix {
			return true
		}
	}

	return false
}
