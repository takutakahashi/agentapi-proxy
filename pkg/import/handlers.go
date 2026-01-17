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
) *Handlers {
	return &Handlers{
		importer:  NewImporter(scheduleManager, webhookRepository),
		exporter:  NewExporter(scheduleManager, webhookRepository),
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
	e.POST("/settings/:org/:team/import", h.ImportTeamResources)
	e.GET("/settings/:org/:team/export", h.ExportTeamResources)

	log.Printf("Registered import/export routes")
	return nil
}

// ImportTeamResources handles POST /settings/:org/:team/import
func (h *Handlers) ImportTeamResources(c echo.Context) error {
	h.setCORSHeaders(c)

	org := c.Param("org")
	team := c.Param("team")
	if org == "" || team == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "org and team are required")
	}
	teamID := fmt.Sprintf("%s-%s", org, team)

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

	// Default mode is create
	if options.Mode == "" {
		options.Mode = ImportModeCreate
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

// ExportTeamResources handles GET /settings/:org/:team/export
func (h *Handlers) ExportTeamResources(c echo.Context) error {
	h.setCORSHeaders(c)

	org := c.Param("org")
	team := c.Param("team")
	if org == "" || team == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "org and team are required")
	}
	teamID := fmt.Sprintf("%s-%s", org, team)

	// Get user from context
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	// Validate user is a member of the team
	if !user.IsMemberOfTeam(teamID) && !user.IsAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "user is not a member of team "+teamID)
	}

	// Parse export options from query parameters
	options := ExportOptions{
		IncludeSecrets: c.QueryParam("include_secrets") == "true",
	}

	// Parse format
	formatStr := c.QueryParam("format")
	if formatStr == "" {
		formatStr = "yaml"
	}
	options.Format = ExportFormat(formatStr)

	// Validate format
	if options.Format != ExportFormatYAML && options.Format != ExportFormatTOML && options.Format != ExportFormatJSON {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid format: must be yaml, toml, or json")
	}

	// Parse status filter
	if statusParam := c.QueryParam("status"); statusParam != "" {
		options.StatusFilter = strings.Split(statusParam, ",")
	}

	// Parse include types
	if includeParam := c.QueryParam("include"); includeParam != "" {
		options.IncludeTypes = strings.Split(includeParam, ",")
	}

	// Perform export
	resources, err := h.exporter.Export(c.Request().Context(), teamID, string(user.ID()), options)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export failed: "+err.Error())
	}

	// Format output
	var buf bytes.Buffer
	if err := h.formatter.Format(resources, options.Format, &buf); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to format output: "+err.Error())
	}

	// Set appropriate content type and filename
	contentType := ContentTypeForFormat(options.Format)
	filename := fmt.Sprintf("%s-export%s", sanitizeFilename(teamID), FileExtensionForFormat(options.Format))

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
