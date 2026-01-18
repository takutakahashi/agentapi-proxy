package importexport

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
)

// Handlers handles import/export endpoints
type Handlers struct {
	importer  *Importer
	exporter  *Exporter
	parser    *Parser
	formatter *Formatter
}

// NewHandlers creates a new Handlers instance
func NewHandlers(
	scheduleManager schedule.Manager,
	webhookRepository repositories.WebhookRepository,
	settingsRepository repositories.SettingsRepository,
	encryptionService services.EncryptionService,
) *Handlers {
	return &Handlers{
		importer:  NewImporter(scheduleManager, webhookRepository, settingsRepository, encryptionService),
		exporter:  NewExporter(scheduleManager, webhookRepository, settingsRepository, encryptionService),
		parser:    NewParser(),
		formatter: NewFormatter(),
	}
}

// GetName returns the name of this handler for logging
func (h *Handlers) GetName() string {
	return "ImportExportHandlers"
}

// RegisterRoutes registers import/export routes
// Implements the app.CustomHandler interface
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
	// GET /manage/:team_id - Export team resources
	e.GET("/manage/:team_id", h.ExportTeamResources)

	// POST /manage/:team_id - Import team resources (create mode)
	e.POST("/manage/:team_id", h.ImportTeamResources)

	// PUT /manage/:team_id - Import team resources (update/upsert mode)
	e.PUT("/manage/:team_id", h.ImportTeamResources)

	log.Printf("Registered team resources management routes: GET/POST/PUT /manage/:team_id")
	return nil
}

// ImportTeamResources handles POST/PUT /manage/:team_id
// POST: Import with create mode (default)
// PUT: Import with upsert mode (default)
func (h *Handlers) ImportTeamResources(c echo.Context) error {
	h.setCORSHeaders(c)

	teamID := c.Param("team_id")
	if teamID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required")
	}

	// Get user from context
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	// Validate user is a member of the team
	if !user.IsMemberOfTeam(teamID) && !user.IsAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "user is not a member of team "+teamID)
	}

	// Parse options from query parameters
	options := ImportOptions{
		DryRun:        c.QueryParam("dry_run") == "true",
		Mode:          ImportMode(c.QueryParam("mode")),
		IDField:       c.QueryParam("id_field"),
		AllowPartial:  c.QueryParam("allow_partial") == "true",
		RegenerateAll: c.QueryParam("regenerate_secrets") == "true",
	}

	// Default mode based on HTTP method
	if options.Mode == "" {
		switch c.Request().Method {
		case "POST":
			options.Mode = ImportModeCreate
		case "PUT":
			options.Mode = ImportModeUpsert
		default:
			options.Mode = ImportModeCreate
		}
	}

	// Default ID field is name
	if options.IDField == "" {
		options.IDField = "name"
	}

	// Validate mode
	if options.Mode != ImportModeCreate && options.Mode != ImportModeUpdate && options.Mode != ImportModeUpsert {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid mode: must be create, update, or upsert")
	}

	// Read request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read request body: "+err.Error())
	}

	// Parse the input
	contentType := c.Request().Header.Get("Content-Type")
	resources, err := h.parser.Parse(body, contentType)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to parse input: "+err.Error())
	}

	// Validate team_id matches
	if resources.Metadata.TeamID != teamID {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf(
			"team_id mismatch: URL has %q but metadata has %q",
			teamID, resources.Metadata.TeamID,
		))
	}

	// Perform import
	result, err := h.importer.Import(c.Request().Context(), resources, string(user.ID()), options)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "import failed: "+err.Error())
	}

	// Return result
	if result.Success {
		return c.JSON(http.StatusOK, result)
	}

	// Partial failure
	return c.JSON(http.StatusMultiStatus, result)
}

// ExportTeamResources handles GET /manage/:team_id
func (h *Handlers) ExportTeamResources(c echo.Context) error {
	h.setCORSHeaders(c)

	teamID := c.Param("team_id")
	if teamID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required")
	}

	// Get user from context
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	// Validate user is a member of the team
	if !user.IsMemberOfTeam(teamID) && !user.IsAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "user is not a member of team "+teamID)
	}

	// Parse format (only query parameter)
	formatStr := c.QueryParam("format")
	if formatStr == "" {
		formatStr = "yaml"
	}
	format := ExportFormat(formatStr)

	// Validate format
	if format != ExportFormatYAML && format != ExportFormatTOML && format != ExportFormatJSON {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid format: must be yaml, toml, or json")
	}

	// Perform export (always includes all resources and secrets, encrypts if available)
	resources, err := h.exporter.Export(c.Request().Context(), teamID, string(user.ID()), ExportOptions{Format: format})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export failed: "+err.Error())
	}

	// Format output
	var buf bytes.Buffer
	if err := h.formatter.Format(resources, format, &buf); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to format output: "+err.Error())
	}

	// Set appropriate content type and filename
	contentType := ContentTypeForFormat(format)
	filename := fmt.Sprintf("%s-export%s", sanitizeFilename(teamID), FileExtensionForFormat(format))

	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	return c.Blob(http.StatusOK, contentType, buf.Bytes())
}

// setCORSHeaders sets CORS headers
func (h *Handlers) setCORSHeaders(c echo.Context) {
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Response().Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// sanitizeFilename sanitizes a filename to remove invalid characters
func sanitizeFilename(s string) string {
	// Replace invalid characters with dash
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
		" ", "-",
	)
	return replacer.Replace(s)
}
