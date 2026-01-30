package mcp

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// Context key for authenticated user
type contextKey string

const userContextKey contextKey = "mcp_authenticated_user"

// MCPHandler implements the CustomHandler interface for MCP endpoints
type MCPHandler struct {
	sessionManager repositories.SessionManager
	shareRepo      repositories.ShareRepository
	httpHandler    http.Handler
}

// NewMCPHandler creates a new MCP handler for the /mcp endpoint
func NewMCPHandler(server *app.Server) *MCPHandler {
	// Get dependencies from server
	sessionManager := server.GetSessionManager()
	shareRepo := server.GetShareRepository()

	// Create HTTP handler using go-sdk's streamable HTTP handler
	// Use stateless mode for simpler session management
	httpOpts := &mcp.StreamableHTTPOptions{
		Stateless: true,
		Logger:    slog.Default(),
	}

	handler := &MCPHandler{
		sessionManager: sessionManager,
		shareRepo:      shareRepo,
	}

	// Create factory function that creates a new MCP server per request with authenticated user
	httpHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		// Extract authenticated user from the request context
		var authenticatedUserID string
		var authenticatedTeams []string
		if user := getUserFromContext(req.Context()); user != nil {
			authenticatedUserID = string(user.ID())
			// Extract team slugs from GitHub user info
			if githubInfo := user.GitHubInfo(); githubInfo != nil {
				for _, team := range githubInfo.Teams() {
					// Format: "org/team-slug"
					teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
					authenticatedTeams = append(authenticatedTeams, teamSlug)
				}
			}
		}

		// Create MCP server with options
		opts := &mcp.ServerOptions{
			Logger: slog.Default(),
			Capabilities: &mcp.ServerCapabilities{
				Tools: &mcp.ToolCapabilities{},
			},
		}

		// Create new MCP server instance with authenticated user
		mcpServer := NewMCPServer(sessionManager, shareRepo, authenticatedUserID, authenticatedTeams, opts)

		// Register all tools
		mcpServer.RegisterTools()

		return mcpServer.GetServer()
	}, httpOpts)

	handler.httpHandler = httpHandler
	return handler
}

// GetName returns the name of this handler for logging
func (h *MCPHandler) GetName() string {
	return "MCPHandler"
}

// RegisterRoutes registers the /mcp endpoint with Echo
func (h *MCPHandler) RegisterRoutes(e *echo.Echo, server *app.Server) error {
	// Register /mcp endpoint with authentication middleware
	// MCP tools require session read permission
	e.Any("/mcp", func(c echo.Context) error {
		// Extract authenticated user from Echo context
		user := auth.GetUserFromContext(c)

		// Store user in http.Request context so MCP server can access it
		ctx := c.Request().Context()
		if user != nil {
			ctx = withUser(ctx, user)
		}

		// Create new request with updated context
		req := c.Request().WithContext(ctx)

		// Delegate to the go-sdk HTTP handler with user in context
		h.httpHandler.ServeHTTP(c.Response(), req)
		return nil
	}, auth.RequirePermission(entities.PermissionSessionRead, server.GetContainer().AuthService))

	log.Printf("[MCP] Registered /mcp endpoint successfully with authentication")
	return nil
}

// withUser stores the user in the context
func withUser(ctx context.Context, user *entities.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// getUserFromContext retrieves the user from the context
func getUserFromContext(ctx context.Context) *entities.User {
	if user, ok := ctx.Value(userContextKey).(*entities.User); ok {
		return user
	}
	return nil
}
