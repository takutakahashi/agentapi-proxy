package githubsync

import (
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
)

// Handlers implements app.CustomHandler for GitHub sync endpoints.
type Handlers struct {
	syncer       *Syncer
	settingsRepo portrepos.SettingsRepository
	kmsKeyARN    string
	awsRegion    string
}

// NewHandlers creates a Handlers instance registering all required repositories.
// kmsKeyARN and awsRegion come from proxy-level config; users cannot override them.
// githubAppInstallID is optional — when set, it enables GitHub App token fallback for users
// who have not configured a personal GitHub token.
func NewHandlers(
	settingsRepo portrepos.SettingsRepository,
	scheduleRepo schedule.Manager,
	webhookRepo portrepos.WebhookRepository,
	memoryRepo portrepos.MemoryRepository,
	taskRepo portrepos.TaskRepository,
	taskGroupRepo portrepos.TaskGroupRepository,
	userFileRepo portrepos.UserFileRepository,
	slackbotRepo portrepos.SlackBotRepository,
	kmsKeyARN, awsRegion string,
	githubAppInstallID string,
) *Handlers {
	return &Handlers{
		syncer:       NewSyncer(settingsRepo, scheduleRepo, webhookRepo, memoryRepo, taskRepo, taskGroupRepo, userFileRepo, slackbotRepo, githubAppInstallID),
		settingsRepo: settingsRepo,
		kmsKeyARN:    kmsKeyARN,
		awsRegion:    awsRegion,
	}
}

// GetName implements app.CustomHandler.
func (h *Handlers) GetName() string { return "GitHubSyncHandlers" }

// RegisterRoutes implements app.CustomHandler.
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
	e.GET("/sync/config/:name", h.GetConfig)
	e.PUT("/sync/config/:name", h.UpdateConfig)
	e.DELETE("/sync/config/:name", h.DeleteConfig)
	e.POST("/sync/push/:name", h.Push)
	e.POST("/sync/pull/:name", h.Pull)
	e.POST("/sync/rotate-key/:name", h.RotateKey)
	log.Printf("[SYNC] Registered GitHub sync endpoints (/sync/*)")
	return nil
}

// GetConfig handles GET /sync/config/:name
func (h *Handlers) GetConfig(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	name := c.Param("name")
	if !h.canAccessSettings(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	settings, err := h.settingsRepo.FindByName(c.Request().Context(), name)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "settings not found")
	}

	gs := settings.GitSync()
	if gs == nil {
		return c.JSON(http.StatusOK, map[string]interface{}{"enabled": false})
	}

	return c.JSON(http.StatusOK, toConfigResponse(gs))
}

// UpdateConfig handles PUT /sync/config/:name
func (h *Handlers) UpdateConfig(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	name := c.Param("name")
	if !h.canModifySettings(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	if h.kmsKeyARN == "" || h.awsRegion == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "GitHub sync encryption is not configured on this proxy")
	}

	var req UpdateSyncConfigRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.RepoFullName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo_full_name is required")
	}

	settings, err := h.settingsRepo.FindByName(c.Request().Context(), name)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "settings not found")
	}

	existing := settings.GitSync()
	encDEK := ""
	dekVersion := 0
	existingToken := ""
	if existing != nil {
		encDEK = existing.Encryption.EncryptedDEK
		dekVersion = existing.Encryption.DEKVersion
		existingToken = existing.GitHubToken
	}

	// Preserve existing token if the request doesn't supply a new one.
	token := req.GitHubToken
	if token == "" {
		token = existingToken
	}

	gs := &entities.GitSyncConfig{
		Enabled:      req.Enabled,
		RepoFullName: req.RepoFullName,
		Branch:       req.Branch,
		RootPath:     req.RootPath,
		AutoPush:     req.AutoPush,
		GitHubToken:  token,
		Encryption: entities.SyncEncryptionConfig{
			KMSKeyARN:    h.kmsKeyARN,
			AWSRegion:    h.awsRegion,
			EncryptedDEK: encDEK,
			DEKVersion:   dekVersion,
		},
	}
	if gs.Branch == "" {
		gs.Branch = "main"
	}
	if gs.RootPath == "" {
		gs.RootPath = "agentapi-config/"
	}

	settings.SetGitSync(gs)
	if err := h.settingsRepo.Save(c.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save config")
	}

	return c.JSON(http.StatusOK, toConfigResponse(gs))
}

// DeleteConfig handles DELETE /sync/config/:name
func (h *Handlers) DeleteConfig(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	name := c.Param("name")
	if !h.canModifySettings(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	settings, err := h.settingsRepo.FindByName(c.Request().Context(), name)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "settings not found")
	}

	settings.SetGitSync(nil)
	if err := h.settingsRepo.Save(c.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save settings")
	}

	return c.JSON(http.StatusOK, map[string]bool{"deleted": true})
}

// Push handles POST /sync/push/:name
func (h *Handlers) Push(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	name := c.Param("name")
	if !h.canModifySettings(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	var req PushRequest
	_ = c.Bind(&req) // optional body

	resp, err := h.syncer.Push(c.Request().Context(), name, string(user.ID()), req.CommitMessage)
	if err != nil {
		log.Printf("[SYNC] push error for %s: %v", name, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("push failed: %v", err))
	}

	return c.JSON(http.StatusOK, resp)
}

// Pull handles POST /sync/pull/:name
func (h *Handlers) Pull(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	name := c.Param("name")
	if !h.canModifySettings(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	var req PullRequest
	_ = c.Bind(&req)

	resp, err := h.syncer.Pull(c.Request().Context(), name, string(user.ID()), req.DeleteOrphans)
	if err != nil {
		log.Printf("[SYNC] pull error for %s: %v", name, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("pull failed: %v", err))
	}

	return c.JSON(http.StatusOK, resp)
}

// RotateKey handles POST /sync/rotate-key/:name
func (h *Handlers) RotateKey(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	name := c.Param("name")
	if !h.canModifySettings(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	resp, err := h.syncer.RotateKey(c.Request().Context(), name, string(user.ID()))
	if err != nil {
		log.Printf("[SYNC] rotate-key error for %s: %v", name, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("rotate-key failed: %v", err))
	}

	return c.JSON(http.StatusOK, resp)
}

// --- access control helpers ---

func (h *Handlers) canAccessSettings(user *entities.User, name string) bool {
	if user.IsAdmin() {
		return true
	}
	if string(user.ID()) == name {
		return true
	}
	return user.IsMemberOfTeam(name)
}

func (h *Handlers) canModifySettings(user *entities.User, name string) bool {
	return h.canAccessSettings(user, name)
}

// toConfigResponse converts GitSyncConfig to the public response (token and KMS details redacted).
func toConfigResponse(gs *entities.GitSyncConfig) *SyncConfigResponse {
	return &SyncConfigResponse{
		Enabled:        gs.Enabled,
		RepoFullName:   gs.RepoFullName,
		Branch:         gs.Branch,
		RootPath:       gs.RootPath,
		AutoPush:       gs.AutoPush,
		HasGitHubToken: gs.GitHubToken != "",
		Encryption: SyncEncryptionResponse{
			DEKVersion: gs.Encryption.DEKVersion,
			DEKReady:   gs.Encryption.EncryptedDEK != "",
		},
	}
}
