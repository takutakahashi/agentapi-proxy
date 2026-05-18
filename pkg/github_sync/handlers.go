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
	syncer *Syncer
}

// NewHandlers creates a Handlers instance registering all required repositories.
// githubAppInstallID is optional — when set, it enables GitHub App token fallback for users
// who have not configured a personal GitHub token.
// kmsKeyARN and awsRegion are no longer used by Handlers directly; they are managed
// by the SettingsController and stored in settings.GitSync.Encryption.
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
		syncer: NewSyncer(settingsRepo, scheduleRepo, webhookRepo, memoryRepo, taskRepo, taskGroupRepo, userFileRepo, slackbotRepo, githubAppInstallID),
	}
}

// GetName implements app.CustomHandler.
func (h *Handlers) GetName() string { return "GitHubSyncHandlers" }

// Syncer returns the underlying Syncer for use by the periodic worker.
func (h *Handlers) Syncer() *Syncer { return h.syncer }

// RegisterRoutes implements app.CustomHandler.
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
	e.POST("/settings/:name/sync/push", h.Push)
	e.POST("/settings/:name/sync/pull", h.Pull)
	e.POST("/settings/:name/sync/rotate-key", h.RotateKey)
	e.POST("/settings/sync/all", h.SyncAll)
	log.Printf("[SYNC] Registered GitHub sync endpoints (/settings/:name/sync/*)")
	return nil
}

// Push handles POST /settings/:name/sync/push
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

// SyncAll handles POST /sync/all (admin only).
// It pushes and/or pulls all settings that have GitHub sync enabled.
func (h *Handlers) SyncAll(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	if !user.IsAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "admin permission required")
	}

	var req SyncAllRequest
	_ = c.Bind(&req)

	resp, err := h.syncer.SyncAll(c.Request().Context(), req.DeleteOrphans, req.CommitMessage)
	if err != nil {
		log.Printf("[SYNC] sync-all error: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("sync-all failed: %v", err))
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
