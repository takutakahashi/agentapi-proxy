package repositories

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// titlePrefix is embedded in memory-server content to carry the agentapi-proxy title field.
// Format: "[TITLE]<title>[/TITLE]\n<content>"
const titlePrefix = "[TITLE]"
const titleSuffix = "[/TITLE]\n"

// tagSep is the separator used to encode map[string]string tags into []string.
// Format: "key=value"
const tagSep = "="

// ExternalMemoryRepository implements MemoryRepository by delegating to an external
// takutakahashi/memory-server instance. Users are created on-demand using the configured
// AdminToken. The user's agentapi-proxy personal API key is registered as their
// memory-server token so that the same key works for both services.
// User tokens are cached in-process.
type ExternalMemoryRepository struct {
	cfg            *config.MemoryExternalConfig
	httpClient     *http.Client
	userTokens     sync.Map // userID (string) -> token (string)
	personalAPIKey portrepos.PersonalAPIKeyRepository
}

// Ensure interface compliance at compile time.
var _ portrepos.MemoryRepository = (*ExternalMemoryRepository)(nil)

// NewExternalMemoryRepository creates a new ExternalMemoryRepository.
// personalAPIKey is used to look up each user's agentapi-proxy API key, which is
// registered as their token in memory-server (same key, two services).
func NewExternalMemoryRepository(cfg *config.MemoryExternalConfig, personalAPIKey portrepos.PersonalAPIKeyRepository) *ExternalMemoryRepository {
	return &ExternalMemoryRepository{
		cfg:            cfg,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		personalAPIKey: personalAPIKey,
	}
}

// ---- memory-server wire types -----------------------------------------------

type msUser struct {
	UserID      string    `json:"user_id"`
	Token       string    `json:"token"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type msMemory struct {
	MemoryID       string    `json:"memory_id"`
	UserID         string    `json:"user_id"`
	Scope          string    `json:"scope"` // "private" | "public"
	Content        string    `json:"content"`
	Tags           []string  `json:"tags"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
	AccessCount    int64     `json:"access_count"`
	VectorID       string    `json:"vector_id"`
}

type msListResult struct {
	Memories  []*msMemory `json:"memories"`
	NextToken *string     `json:"next_token,omitempty"`
}

// ---- user management --------------------------------------------------------

// resolveUserID returns the appropriate memory-server user ID for a given agentapi-proxy user.
// For team-scoped operations the convention "team:<teamID>" is used so that team memories
// are owned by a dedicated team user rather than an individual.
func resolveUserID(scope entities.ResourceScope, ownerID, teamID string) string {
	if scope == entities.ScopeTeam && teamID != "" {
		return "team:" + teamID
	}
	return ownerID
}

// errUserNotFound is returned by getUser when the user does not exist (HTTP 404).
var errUserNotFound = fmt.Errorf("user not found")

// ensureUser gets or creates a user in memory-server and caches their token.
// It only calls createUser when the user provably does not exist (HTTP 404).
// Any other error from getUser is propagated without attempting creation, to
// avoid accidentally overwriting an existing user's token on transient failures.
func (r *ExternalMemoryRepository) ensureUser(ctx context.Context, userID string) (string, error) {
	if token, ok := r.userTokens.Load(userID); ok {
		return token.(string), nil
	}

	user, err := r.getUser(ctx, userID)
	if err != nil {
		if !errors.Is(err, errUserNotFound) {
			// Transient error (network, 5xx, …) — do NOT overwrite existing token.
			return "", fmt.Errorf("ensure user %q: get failed: %w", userID, err)
		}
		// User does not exist yet — safe to create.
		user, err = r.createUser(ctx, userID)
		if err != nil {
			return "", fmt.Errorf("ensure user %q: create failed: %w", userID, err)
		}
	}

	r.userTokens.Store(userID, user.Token)
	return user.Token, nil
}

func (r *ExternalMemoryRepository) getUser(ctx context.Context, userID string) (*msUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		r.cfg.URL+"/api/v1/users/"+url.PathEscape(userID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.AdminToken)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, errUserNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get user: HTTP %d", resp.StatusCode)
	}

	var u msUser
	return &u, json.NewDecoder(resp.Body).Decode(&u)
}

// lookupAPIKey returns the user's personal API key string, or "" if not found.
func (r *ExternalMemoryRepository) lookupAPIKey(ctx context.Context, userID string) string {
	if r.personalAPIKey == nil {
		return ""
	}
	key, err := r.personalAPIKey.FindByUserID(ctx, entities.UserID(userID))
	if err != nil || key == nil {
		return ""
	}
	return key.APIKey()
}

func (r *ExternalMemoryRepository) createUser(ctx context.Context, userID string) (*msUser, error) {
	payload := map[string]string{
		"user_id":     userID,
		"description": "agentapi-proxy managed user",
	}
	// Register the user's agentapi-proxy personal API key as their memory-server token.
	// Falls back to auto-generated token when no personal API key exists.
	if apiKey := r.lookupAPIKey(ctx, userID); apiKey != "" {
		payload["token"] = apiKey
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.URL+"/api/v1/users", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.AdminToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create user: HTTP %d", resp.StatusCode)
	}

	var u msUser
	return &u, json.NewDecoder(resp.Body).Decode(&u)
}

// ---- content encoding -------------------------------------------------------

// encodeContent packs the agentapi-proxy title into the memory-server content field.
func encodeContent(title, content string) string {
	return titlePrefix + title + titleSuffix + content
}

// decodeContent extracts title and content from a memory-server content field.
func decodeContent(raw string) (title, content string) {
	if !strings.HasPrefix(raw, titlePrefix) {
		return "", raw
	}
	rest := strings.TrimPrefix(raw, titlePrefix)
	idx := strings.Index(rest, titleSuffix)
	if idx < 0 {
		return "", raw
	}
	return rest[:idx], rest[idx+len(titleSuffix):]
}

// encodeTags converts map[string]string to []string ("key=value").
func encodeTags(tags map[string]string) []string {
	result := make([]string, 0, len(tags))
	for k, v := range tags {
		result = append(result, k+tagSep+v)
	}
	return result
}

// decodeTags converts []string ("key=value") back to map[string]string.
func decodeTags(tags []string) map[string]string {
	result := make(map[string]string, len(tags))
	for _, t := range tags {
		idx := strings.Index(t, tagSep)
		if idx < 0 {
			result[t] = ""
			continue
		}
		result[t[:idx]] = t[idx+len(tagSep):]
	}
	return result
}

// scopeToMS converts agentapi-proxy ResourceScope to memory-server scope string.
// Team-scoped memories are stored as "private" under a dedicated team user.
func scopeToMS(s entities.ResourceScope) string {
	// Both user and team scopes are stored as "private" in memory-server.
	// Access control is handled by using separate user accounts per owner/team.
	return "private"
}

// msToEntity converts a memory-server memory into an agentapi-proxy Memory entity.
func msToEntity(m *msMemory) *entities.Memory {
	title, content := decodeContent(m.Content)
	tags := decodeTags(m.Tags)

	// Infer scope and IDs from UserID convention ("team:<teamID>" or plain user ID).
	scope := entities.ScopeUser
	ownerID := m.UserID
	teamID := ""
	if strings.HasPrefix(m.UserID, "team:") {
		scope = entities.ScopeTeam
		teamID = strings.TrimPrefix(m.UserID, "team:")
		ownerID = teamID // best effort; real owner is unknown at this level
	}

	mem := entities.NewMemoryWithTags(m.MemoryID, title, content, scope, ownerID, teamID, tags)
	mem.SetCreatedAt(m.CreatedAt)
	mem.SetUpdatedAt(m.UpdatedAt)
	return mem
}

// ---- MemoryRepository interface implementation ------------------------------

// msAddResult is the response body from POST /api/v1/memories.
type msAddResult struct {
	MemoryID string `json:"memory_id"`
}

// Create persists a new memory entry in memory-server.
// memory-server generates its own UUID; after creation the entity's ID is
// overwritten with the server-assigned ID so that subsequent GetByID/Update/Delete
// calls use the correct ID.
func (r *ExternalMemoryRepository) Create(ctx context.Context, memory *entities.Memory) error {
	if err := memory.Validate(); err != nil {
		return fmt.Errorf("invalid memory: %w", err)
	}

	msUserID := resolveUserID(memory.Scope(), memory.OwnerID(), memory.TeamID())
	token, err := r.ensureUser(ctx, msUserID)
	if err != nil {
		return err
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"content": encodeContent(memory.Title(), memory.Content()),
		"tags":    encodeTags(memory.Tags()),
		"scope":   scopeToMS(memory.Scope()),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.URL+"/api/v1/memories", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create memory: HTTP %d", resp.StatusCode)
	}

	// Overwrite the caller-supplied placeholder ID with the server-assigned UUID.
	var result msAddResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode create response: %w", err)
	}
	if result.MemoryID != "" {
		memory.SetID(result.MemoryID)
	}

	return nil
}

// GetByID retrieves a memory entry by its ID.
func (r *ExternalMemoryRepository) GetByID(ctx context.Context, id string) (*entities.Memory, error) {
	// memory-server GET /api/v1/memories/{id} does not require auth by default,
	// but we send the admin token to ensure access.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		r.cfg.URL+"/api/v1/memories/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.AdminToken)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, entities.ErrMemoryNotFound{ID: id}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get memory %q: HTTP %d", id, resp.StatusCode)
	}

	var m msMemory
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return msToEntity(&m), nil
}

// List retrieves memory entries matching the given filter.
// Full-text query uses memory-server's semantic search endpoint; otherwise the
// list endpoint is used with client-side tag/scope filtering.
func (r *ExternalMemoryRepository) List(ctx context.Context, filter portrepos.MemoryFilter) ([]*entities.Memory, error) {
	// Collect all relevant user IDs to query.
	userIDs := r.filterUserIDs(filter)
	if len(userIDs) == 0 {
		return nil, nil
	}

	var results []*entities.Memory
	seen := make(map[string]struct{})

	for _, uid := range userIDs {
		token, err := r.ensureUser(ctx, uid)
		if err != nil {
			// Skip users that cannot be provisioned.
			continue
		}

		var memories []*msMemory
		if filter.Query != "" {
			memories, err = r.searchMemories(ctx, token, uid, filter.Query)
		} else {
			memories, err = r.listAllMemories(ctx, token, uid)
		}
		if err != nil {
			return nil, err
		}

		for _, m := range memories {
			if _, dup := seen[m.MemoryID]; dup {
				continue
			}
			seen[m.MemoryID] = struct{}{}

			entity := msToEntity(m)
			if !entity.MatchesTags(filter.Tags) {
				continue
			}
			if len(filter.ExcludeTags) > 0 && entity.MatchesTags(filter.ExcludeTags) {
				continue
			}
			results = append(results, entity)
		}
	}

	return results, nil
}

// filterUserIDs returns the list of memory-server user IDs to query for the given filter.
func (r *ExternalMemoryRepository) filterUserIDs(filter portrepos.MemoryFilter) []string {
	var ids []string

	// User-scope memories.
	if filter.Scope == "" || filter.Scope == entities.ScopeUser {
		if filter.OwnerID != "" {
			ids = append(ids, filter.OwnerID)
		}
	}

	// Team-scope memories.
	if filter.Scope == "" || filter.Scope == entities.ScopeTeam {
		if len(filter.TeamIDs) > 0 {
			for _, tid := range filter.TeamIDs {
				ids = append(ids, "team:"+tid)
			}
		} else if filter.TeamID != "" {
			ids = append(ids, "team:"+filter.TeamID)
		}
	}

	return ids
}

// listAllMemories fetches all memories for a user by paginating through the list endpoint.
func (r *ExternalMemoryRepository) listAllMemories(ctx context.Context, token, userID string) ([]*msMemory, error) {
	var all []*msMemory
	var nextToken *string

	for {
		u := r.cfg.URL + "/api/v1/memories?user_id=" + url.QueryEscape(userID) + "&limit=100"
		if nextToken != nil {
			u += "&next_token=" + url.QueryEscape(*nextToken)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := r.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list memories: HTTP %d", resp.StatusCode)
		}

		var result msListResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		all = append(all, result.Memories...)

		if result.NextToken == nil {
			break
		}
		nextToken = result.NextToken
	}

	return all, nil
}

// searchMemories performs semantic search in memory-server.
func (r *ExternalMemoryRepository) searchMemories(ctx context.Context, token, userID, query string) ([]*msMemory, error) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"user_id": userID,
		"query":   query,
		"limit":   50,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.URL+"/api/v1/memories/search", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search memories: HTTP %d", resp.StatusCode)
	}

	// Search returns []SearchResult{Memory, SimilarityScore, FinalScore}.
	var results []struct {
		Memory *msMemory `json:"memory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	memories := make([]*msMemory, 0, len(results))
	for _, sr := range results {
		if sr.Memory != nil {
			memories = append(memories, sr.Memory)
		}
	}
	return memories, nil
}

// Update replaces an existing memory entry's content/tags/scope.
func (r *ExternalMemoryRepository) Update(ctx context.Context, memory *entities.Memory) error {
	msUserID := resolveUserID(memory.Scope(), memory.OwnerID(), memory.TeamID())
	token, err := r.ensureUser(ctx, msUserID)
	if err != nil {
		return err
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"content": encodeContent(memory.Title(), memory.Content()),
		"tags":    encodeTags(memory.Tags()),
		"scope":   scopeToMS(memory.Scope()),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		r.cfg.URL+"/api/v1/memories/"+url.PathEscape(memory.ID()),
		bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return entities.ErrMemoryNotFound{ID: memory.ID()}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update memory %q: HTTP %d", memory.ID(), resp.StatusCode)
	}

	return nil
}

// Delete removes a memory entry.
func (r *ExternalMemoryRepository) Delete(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		r.cfg.URL+"/api/v1/memories/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.AdminToken)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return entities.ErrMemoryNotFound{ID: id}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete memory %q: HTTP %d", id, resp.StatusCode)
	}

	return nil
}
