package controllers

import (
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// FileController handles user-managed file endpoints.
type FileController struct {
	repo portrepos.UserFileRepository
}

// NewFileController creates a new FileController.
func NewFileController(repo portrepos.UserFileRepository) *FileController {
	return &FileController{repo: repo}
}

// GetName returns the controller name for logging.
func (c *FileController) GetName() string { return "FileController" }

// CreateFileRequest is the request body for creating or updating a file.
type CreateFileRequest struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	Permissions string `json:"permissions"`
}

// UpdateFileRequest is the request body for updating an existing file.
type UpdateFileRequest struct {
	Name        *string `json:"name"`
	Path        *string `json:"path"`
	Content     *string `json:"content"`
	Permissions *string `json:"permissions"`
}

// FileResponse is the response body for a single file.
type FileResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	Permissions string `json:"permissions"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ListFilesResponse is the response body for listing files.
type ListFilesResponse struct {
	Files []FileResponse `json:"files"`
}

// CreateFile handles POST /files
func (c *FileController) CreateFile(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req CreateFileRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}
	if req.Path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "path is required")
	}

	fileID := uuid.New().String()
	file := entities.NewUserFile(fileID, req.Name, req.Path, req.Content, req.Permissions)

	userID := string(user.ID())
	if err := c.repo.Save(ctx.Request().Context(), userID, file); err != nil {
		log.Printf("[FILES] Failed to save file for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save file")
	}

	return ctx.JSON(http.StatusCreated, toFileResponse(file))
}

// ListFiles handles GET /files
func (c *FileController) ListFiles(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := string(user.ID())
	files, err := c.repo.List(ctx.Request().Context(), userID)
	if err != nil {
		log.Printf("[FILES] Failed to list files for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list files")
	}

	resp := ListFilesResponse{Files: make([]FileResponse, 0, len(files))}
	for _, f := range files {
		resp.Files = append(resp.Files, toFileResponse(f))
	}
	return ctx.JSON(http.StatusOK, resp)
}

// GetFile handles GET /files/:fileId
func (c *FileController) GetFile(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	fileID := ctx.Param("fileId")
	if fileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "fileId is required")
	}

	userID := string(user.ID())
	file, err := c.repo.FindByID(ctx.Request().Context(), userID, fileID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "File not found")
		}
		log.Printf("[FILES] Failed to get file %s for user %s: %v", fileID, userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get file")
	}

	return ctx.JSON(http.StatusOK, toFileResponse(file))
}

// UpdateFile handles PUT /files/:fileId
func (c *FileController) UpdateFile(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	fileID := ctx.Param("fileId")
	if fileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "fileId is required")
	}

	var req UpdateFileRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	userID := string(user.ID())
	file, err := c.repo.FindByID(ctx.Request().Context(), userID, fileID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "File not found")
		}
		log.Printf("[FILES] Failed to find file %s for user %s: %v", fileID, userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find file")
	}

	if req.Name != nil {
		file.SetName(*req.Name)
	}
	if req.Path != nil {
		if *req.Path == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "path must not be empty")
		}
		file.SetPath(*req.Path)
	}
	if req.Content != nil {
		file.SetContent(*req.Content)
	}
	if req.Permissions != nil {
		file.SetPermissions(*req.Permissions)
	}

	if err := c.repo.Save(ctx.Request().Context(), userID, file); err != nil {
		log.Printf("[FILES] Failed to update file %s for user %s: %v", fileID, userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update file")
	}

	return ctx.JSON(http.StatusOK, toFileResponse(file))
}

// DeleteFile handles DELETE /files/:fileId
func (c *FileController) DeleteFile(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	fileID := ctx.Param("fileId")
	if fileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "fileId is required")
	}

	userID := string(user.ID())
	if err := c.repo.Delete(ctx.Request().Context(), userID, fileID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "File not found")
		}
		log.Printf("[FILES] Failed to delete file %s for user %s: %v", fileID, userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete file")
	}

	return ctx.JSON(http.StatusOK, map[string]bool{"success": true})
}

// toFileResponse converts a UserFile entity to a FileResponse.
func toFileResponse(f *entities.UserFile) FileResponse {
	return FileResponse{
		ID:          f.ID(),
		Name:        f.Name(),
		Path:        f.Path(),
		Content:     f.Content(),
		Permissions: f.Permissions(),
		CreatedAt:   f.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   f.UpdatedAt().Format("2006-01-02T15:04:05Z07:00"),
	}
}
