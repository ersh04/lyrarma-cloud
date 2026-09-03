package managers

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/httpresponse"
	"github.com/lyrarma/cloud-api/src/middleware"
	"github.com/lyrarma/cloud-api/src/models"
	"github.com/lyrarma/cloud-api/src/storage"
)

// FolderHandler manages the folder hierarchy and bulk file operations.
type FolderHandler struct {
	metadataStore *storage.SQLStore
	fileStore     *storage.S3Store
	logger        *slog.Logger
}

// NewFolderHandler creates a folder handler with the provided dependencies.
func NewFolderHandler(
	metadataStore *storage.SQLStore,
	fileStore *storage.S3Store,
	logger *slog.Logger,
) *FolderHandler {
	return &FolderHandler{
		metadataStore: metadataStore,
		fileStore:     fileStore,
		logger:        logger,
	}
}

// List returns all folders belonging to the authenticated user.
func (h *FolderHandler) List(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	folders, err := h.metadataStore.ListFolders(c.Context(), userID)
	if err != nil {
		h.logger.Error("failed to retrieve folder list", "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to retrieve folder list")
		return
	}
	httpresponse.WriteJSON(c, fiber.StatusOK, models.FolderListResponse{
		Folders: folders,
		Total:   len(folders),
	})
}

// Create creates a folder inside the selected parent folder.
func (h *FolderHandler) Create(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" || len(name) > 128 {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "validation_error", "folder name must be between 1 and 128 characters")
		return
	}

	entry := models.FolderEntry{
		ID:        generateID(),
		OwnerID:   userID,
		ParentID:  models.NormalizeFolderID(c.FormValue("parent_id")),
		Name:      name,
		IsPublic:  false,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.metadataStore.CreateFolder(c.Context(), entry); err != nil {
		switch {
		case errors.Is(err, storage.ErrFolderAlreadyExists):
			httpresponse.WriteError(c, fiber.StatusConflict, "folder_exists", "folder already exists")
		case errors.Is(err, storage.ErrFolderNotFound):
			httpresponse.WriteError(c, fiber.StatusNotFound, "parent_not_found", "parent folder not found")
		default:
			h.logger.Error("folder creation failed", "user_id", userID, "err", err)
			httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to create folder")
		}
		return
	}

	h.logger.Info("folder created", "folder_id", entry.ID, "user_id", userID, "name", entry.Name)
	httpresponse.WriteJSON(c, fiber.StatusCreated, entry)
}

// ChangeFolderPermission changes whether a folder is public.
func (h *FolderHandler) ChangeFolderPermission(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	folderID := strings.TrimSpace(c.Params("folderID"))
	if folderID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "folderID is missing")
		return
	}

	isPublic, err := strconv.ParseBool(strings.TrimSpace(c.Params("isPublic")))
	if err != nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "validation_error", "isPublic must be true or false")
		return
	}

	if err := h.metadataStore.UpdateFolderPermission(
		c.Context(),
		userID,
		folderID,
		isPublic,
	); err != nil {
		if errors.Is(err, storage.ErrFolderNotFound) {
			httpresponse.WriteError(c, fiber.StatusNotFound, "not_found", "folder not found")
			return
		}
		h.logger.Error("folder access update failed", "folder_id", folderID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to update access")
		return
	}

	h.logger.Info("folder permissions updated", "folder_id", folderID, "user_id", userID, "is_public", isPublic)
	httpresponse.WriteJSON(c, fiber.StatusOK, models.SuccessResponse{Message: "access permissions updated"})
}

// Delete removes a folder together with all its files and nested folders.
func (h *FolderHandler) Delete(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	folderID := strings.TrimSpace(c.Params("folderID"))
	if folderID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "folderID is missing")
		return
	}

	files, err := h.metadataStore.DeleteFolder(c.Context(), userID, folderID)
	if err != nil {
		if errors.Is(err, storage.ErrFolderNotFound) {
			httpresponse.WriteError(c, fiber.StatusNotFound, "not_found", "folder not found")
			return
		}
		h.logger.Error("folder deletion failed", "folder_id", folderID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to delete folder")
		return
	}

	for _, entry := range files {
		if err := h.fileStore.DeleteFile(
			c.Context(),
			fileObjectKey(entry.OwnerID, entry.ID),
		); err != nil {
			h.logger.Error(
				"orphaned S3 object after folder deletion",
				"folder_id", folderID,
				"file_id", entry.ID,
				"err", err,
			)
		}
	}

	h.logger.Info("folder deleted", "folder_id", folderID, "user_id", userID, "files", len(files))
	httpresponse.WriteJSON(c, fiber.StatusOK, models.SuccessResponse{Message: "folder deleted"})
}

// GetPublicFolderInfo returns metadata for a public folder.
func (h *FolderHandler) GetPublicFolderInfo(c fiber.Ctx) {
	folderID := strings.TrimSpace(c.Params("folderID"))
	if folderID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "folderID is missing")
		return
	}

	folder, err := h.metadataStore.GetPublicFolder(c.Context(), folderID)
	if err != nil {
		if errors.Is(err, storage.ErrFolderNotFound) {
			httpresponse.WriteError(c, fiber.StatusNotFound, "not_found", "folder not found")
			return
		}
		h.logger.Error("failed to retrieve public folder", "folder_id", folderID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to retrieve folder")
		return
	}
	httpresponse.WriteJSON(c, fiber.StatusOK, folder)
}

// DownloadPublicFolder streams a public folder as a ZIP archive.
func (h *FolderHandler) DownloadPublicFolder(c fiber.Ctx) {
	folderID := strings.TrimSpace(c.Params("folderID"))
	if folderID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "folderID is missing")
		return
	}

	root, err := h.metadataStore.GetPublicFolder(c.Context(), folderID)
	if err != nil {
		if errors.Is(err, storage.ErrFolderNotFound) {
			httpresponse.WriteError(c, fiber.StatusNotFound, "not_found", "folder not found")
			return
		}
		h.logger.Error("failed to retrieve public folder", "folder_id", folderID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to retrieve folder")
		return
	}

	folders, err := h.metadataStore.FolderTree(c.Context(), root.OwnerID, root.ID)
	if err != nil {
		h.logger.Error("failed to retrieve folder tree", "folder_id", root.ID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to retrieve folder tree")
		return
	}
	files, err := h.metadataStore.ListFiles(c.Context(), root.OwnerID, "")
	if err != nil {
		h.logger.Error("failed to retrieve folder files", "folder_id", root.ID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to retrieve folder files")
		return
	}

	folderPaths := archiveFolderPaths(root, folders)
	archiveName := safeName(root.Name, "download") + ".zip"
	ctx := c.Context()
	c.Set("Content-Type", "application/zip")
	c.Set(
		"Content-Disposition",
		mime.FormatMediaType("attachment", map[string]string{"filename": archiveName}),
	)

	_ = c.SendStreamWriter(func(writer *bufio.Writer) {
		archive := zip.NewWriter(writer)
		if err := writeFolderArchive(
			ctx,
			archive,
			h.fileStore,
			files,
			folderPaths,
			h.logger,
		); err != nil {
			h.logger.Error("ZIP archive creation failed", "folder_id", root.ID, "err", err)
		}
		if err := archive.Close(); err != nil {
			h.logger.Error("ZIP archive closing failed", "folder_id", root.ID, "err", err)
		}
	})
}

func writeFolderArchive(
	ctx context.Context,
	archive *zip.Writer,
	fileStore *storage.S3Store,
	files []models.FileEntry,
	folderPaths map[string]string,
	logger *slog.Logger,
) error {
	directories := make([]string, 0, len(folderPaths))
	for _, folderPath := range folderPaths {
		if folderPath != "" {
			directories = append(directories, folderPath)
		}
	}
	sort.Strings(directories)
	for _, directory := range directories {
		header := &zip.FileHeader{
			Name:   strings.TrimSuffix(directory, "/") + "/",
			Method: zip.Store,
		}
		if _, err := archive.CreateHeader(header); err != nil {
			return err
		}
	}

	sort.Slice(files, func(i, j int) bool {
		left := path.Join(folderPaths[files[i].FolderID], files[i].OriginalName, files[i].ID)
		right := path.Join(folderPaths[files[j].FolderID], files[j].OriginalName, files[j].ID)
		return left < right
	})

	for _, entry := range files {
		folderPath, included := folderPaths[entry.FolderID]
		if !included {
			continue
		}

		file, _, err := fileStore.DownloadFile(ctx, fileObjectKey(entry.OwnerID, entry.ID))
		if err != nil {
			logger.Warn("file skipped while creating archive", "file_id", entry.ID, "err", err)
			continue
		}

		header := &zip.FileHeader{
			Name:     path.Join(folderPath, safeName(entry.OriginalName, "download")),
			Method:   zip.Deflate,
			Modified: entry.UploadedAt,
		}
		writer, createErr := archive.CreateHeader(header)
		if createErr == nil {
			_, createErr = io.Copy(writer, file)
		}
		closeErr := file.Close()
		if createErr != nil {
			return createErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func archiveFolderPaths(
	root models.FolderEntry,
	folders []models.FolderEntry,
) map[string]string {
	byID := make(map[string]models.FolderEntry, len(folders))
	for _, folder := range folders {
		byID[folder.ID] = folder
	}

	paths := make(map[string]string, len(folders))
	visiting := make(map[string]bool, len(folders))
	var resolve func(string) string
	resolve = func(id string) string {
		if value, found := paths[id]; found {
			return value
		}
		if visiting[id] {
			return ""
		}

		folder, found := byID[id]
		if !found {
			return ""
		}
		visiting[id] = true
		defer delete(visiting, id)

		if folder.ID == root.ID {
			paths[id] = safeName(root.Name, "download")
			return paths[id]
		}

		parent := resolve(folder.ParentID)
		if parent == "" {
			return ""
		}
		paths[id] = path.Join(parent, safeName(folder.Name, "download"))
		return paths[id]
	}

	for id := range byID {
		resolve(id)
	}
	return paths
}
