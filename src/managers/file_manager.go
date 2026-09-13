package managers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/httpresponse"
	"github.com/lyrarma/cloud-api/src/middleware"
	"github.com/lyrarma/cloud-api/src/models"
	"github.com/lyrarma/cloud-api/src/storage"
)

// FileHandler manages file operations and connects PostgreSQL with S3.
type FileHandler struct {
	metadataStore *storage.SQLStore
	s3Storage     *storage.S3Store
	limits        config.UserLimitSet
	logger        *slog.Logger
}

// NewFileHandler creates a file handler with the provided dependencies.
func NewFileHandler(
	metadataStore *storage.SQLStore,
	s3Storage *storage.S3Store,
	limits config.UserLimitSet,
	logger *slog.Logger,
) *FileHandler {
	return &FileHandler{
		metadataStore: metadataStore,
		s3Storage:     s3Storage,
		limits:        limits,
		logger:        logger,
	}
}

// FileList returns authenticated user files with optional folder filtering.
func (h *FileHandler) FileList(c fiber.Ctx) error {
	userID := middleware.UserIDFromContext(c.Context())
	folderID := models.NormalizeFolderID(c.Query("folder_id"))

	entries, err := h.metadataStore.ListFiles(c.Context(), userID, folderID)
	if err != nil {
		h.logger.Error("failed to retrieve file list", "user_id", userID, "folder_id", folderID, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "internal_error",
			"message": "failed to retrieve file list",
		})
	}

	return c.Status(fiber.StatusOK).JSON(models.FileListResponse{
		Files: entries,
		Total: len(entries),
	})
}

// UploadFile validates quota, uploads a multipart file to S3, and stores its metadata.
func (h *FileHandler) UploadFile(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	folderID := models.NormalizeFolderID(c.Query("folder_id"))

	userLimits, err := h.userLimits(c.Context(), userID)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			httpresponse.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "user no longer exists")
			return
		}
		h.logger.Error("failed to determine user limits", "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to determine upload limits")
		return
	}

	if folderID != "root" {
		if _, err := h.metadataStore.GetFolder(c.Context(), userID, folderID); err != nil {
			if errors.Is(err, storage.ErrFolderNotFound) {
				httpresponse.WriteError(c, fiber.StatusNotFound, "folder_not_found", "folder not found")
				return
			}
			h.logger.Error("failed to validate upload folder", "folder_id", folderID, "err", err)
			httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to validate folder")
			return
		}
	}

	req := c.Request()
	if req == nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "invalid_request", "request is nil")
		return
	}

	form, err := req.MultipartForm()
	if err != nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "parse_error", "invalid multipart form")
		return
	}
	defer form.RemoveAll()

	header, err := c.FormFile("file")
	if err != nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_file", "the 'file' field is missing from the form")
		return
	}

	file, err := header.Open()
	if err != nil {
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to open uploaded file")
		return
	}

	defer file.Close()

	fileName := safeName(header.Filename, "")
	if fileName == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "validation_error", "file name is invalid")
		return
	}

	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		h.logger.Error("failed to determine upload size", "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to read uploaded file")
		return
	}
	if !userLimits.AllowsFile(size) {
		httpresponse.WriteError(c, fiber.StatusRequestEntityTooLarge, "too_large", "file size exceeds the allowed limit")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.logger.Error("failed to rewind uploaded file", "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to read uploaded file")
		return
	}

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	entry := models.FileEntry{
		ID:           generateID(),
		OwnerID:      userID,
		FolderID:     folderID,
		OriginalName: fileName,
		Size:         size,
		ContentType:  contentType,
		UploadedAt:   time.Now().UTC(),
	}
	objectKey := fileObjectKey(entry.OwnerID, entry.ID)
	reservationID := generateID()
	if err := h.metadataStore.ReserveUpload(
		c.Context(),
		userID,
		reservationID,
		size,
		userLimits.MaxStorageBytes(),
	); err != nil {
		if errors.Is(err, storage.ErrStorageQuotaExceeded) {
			httpresponse.WriteError(c, fiber.StatusInsufficientStorage, "storage_quota_exceeded", "storage quota exceeded")
			return
		}
		h.logger.Error("failed to reserve storage quota", "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to reserve storage quota")
		return
	}

	reservationActive := true
	defer func() {
		if !reservationActive {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.metadataStore.ReleaseUploadReservation(cleanupContext, userID, reservationID); err != nil {
			h.logger.Error("failed to release storage reservation", "user_id", userID, "reservation_id", reservationID, "err", err)
		}
	}()

	if err := h.s3Storage.UploadFile(c.Context(), objectKey, file, entry.ContentType); err != nil {
		h.logger.Error("file upload to S3 failed", "file_id", entry.ID, "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "save_error", "failed to save file")
		return
	}

	if err := h.metadataStore.CommitReservedFile(c.Context(), entry, reservationID); err != nil {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupErr := h.s3Storage.DeleteFile(cleanupContext, objectKey)
		cancel()
		if cleanupErr != nil {
			h.logger.Error("failed to roll back S3 upload", "file_id", entry.ID, "err", cleanupErr)
		}
		if errors.Is(err, storage.ErrFolderNotFound) {
			httpresponse.WriteError(c, fiber.StatusNotFound, "folder_not_found", "folder not found")
			return
		}
		h.logger.Error("file metadata save failed", "file_id", entry.ID, "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "save_error", "failed to save file")
		return
	}
	reservationActive = false

	h.logger.Info(
		"file uploaded",
		"file_id", entry.ID,
		"user_id", userID,
		"name", entry.OriginalName,
		"size", entry.Size,
	)
	httpresponse.WriteJSON(c, fiber.StatusCreated, entry)
}

// DownloadFile sends the contents of a file to its owner.
func (h *FileHandler) DownloadFile(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	fileID := strings.TrimSpace(c.Params("fileID"))
	if fileID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "fileID is missing")
		return
	}

	entry, err := h.metadataStore.GetFile(c.Context(), userID, fileID)
	if err != nil {
		h.handleFileLookupError(c, "failed to retrieve file", fileID, err)
		return
	}
	h.streamFile(c, entry)
}

// DownloadPublicFile sends a public file without authentication.
func (h *FileHandler) DownloadPublicFile(c fiber.Ctx) {
	fileID := strings.TrimSpace(c.Params("fileID"))
	if fileID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "fileID is missing")
		return
	}

	entry, err := h.metadataStore.GetPublicFile(c.Context(), fileID)
	if err != nil {
		h.handleFileLookupError(c, "failed to retrieve public file", fileID, err)
		return
	}
	h.streamFile(c, entry)
}

func (h *FileHandler) streamFile(c fiber.Ctx, entry models.FileEntry) {
	body, _, err := h.s3Storage.DownloadFile(
		c.Context(),
		fileObjectKey(entry.OwnerID, entry.ID),
	)
	if err != nil {
		h.logger.Error("failed to download file from S3", "file_id", entry.ID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to download file")
		return
	}

	contentType := entry.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Set("Content-Type", contentType)
	c.Set(
		"Content-Disposition",
		mime.FormatMediaType("attachment", map[string]string{"filename": entry.OriginalName}),
	)

	if err := c.SendStream(body, int(entry.Size)); err != nil {
		_ = body.Close()
		h.logger.Error("file streaming failed", "file_id", entry.ID, "err", err)
	}
}

// GetPublicFileInfo returns metadata for a public file.
func (h *FileHandler) GetPublicFileInfo(c fiber.Ctx) {
	fileID := strings.TrimSpace(c.Params("fileID"))
	if fileID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "fileID is missing")
		return
	}

	entry, err := h.metadataStore.GetPublicFile(c.Context(), fileID)
	if err != nil {
		h.handleFileLookupError(c, "failed to retrieve public file info", fileID, err)
		return
	}
	httpresponse.WriteJSON(c, fiber.StatusOK, entry)
}

// FileInfo returns metadata for a file owned by the user.
func (h *FileHandler) FileInfo(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	fileID := strings.TrimSpace(c.Params("fileID"))
	if fileID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "fileID is missing")
		return
	}

	entry, err := h.metadataStore.GetFile(c.Context(), userID, fileID)
	if err != nil {
		h.handleFileLookupError(c, "failed to retrieve file info", fileID, err)
		return
	}
	httpresponse.WriteJSON(c, fiber.StatusOK, entry)
}

// DeleteFile removes file metadata and the corresponding S3 object.
func (h *FileHandler) DeleteFile(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	fileID := strings.TrimSpace(c.Params("fileID"))
	if fileID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "fileID is missing")
		return
	}

	entry, err := h.metadataStore.DeleteFile(c.Context(), userID, fileID)
	if err != nil {
		h.handleFileLookupError(c, "file deletion failed", fileID, err)
		return
	}

	if err := h.s3Storage.DeleteFile(c.Context(), fileObjectKey(entry.OwnerID, entry.ID)); err != nil {
		h.logger.Error("orphaned S3 object after metadata deletion", "file_id", entry.ID, "err", err)
	}

	h.logger.Info("file deleted", "file_id", fileID, "user_id", userID)
	httpresponse.WriteJSON(c, fiber.StatusOK, models.SuccessResponse{Message: "file deleted"})
}

// ChangeFilePermission changes whether an owned file is public.
func (h *FileHandler) ChangeFilePermission(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	fileID := strings.TrimSpace(c.Params("fileID"))
	if fileID == "" {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "missing_param", "fileID is missing")
		return
	}

	isPublic, err := strconv.ParseBool(strings.TrimSpace(c.Params("isPublic")))
	if err != nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "validation_error", "isPublic must be true or false")
		return
	}

	if err := h.metadataStore.UpdateFilePermission(c.Context(), userID, fileID, isPublic); err != nil {
		h.handleFileLookupError(c, "file permission update failed", fileID, err)
		return
	}

	h.logger.Info("file permissions updated", "file_id", fileID, "user_id", userID, "is_public", isPublic)
	httpresponse.WriteJSON(c, fiber.StatusOK, models.SuccessResponse{Message: "access permissions updated"})
}

func (h *FileHandler) handleFileLookupError(
	c fiber.Ctx,
	logMessage string,
	fileID string,
	err error,
) {
	if errors.Is(err, storage.ErrFileNotFound) {
		httpresponse.WriteError(c, fiber.StatusNotFound, "not_found", "file not found")
		return
	}
	h.logger.Error(logMessage, "file_id", fileID, "err", err)
	httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
}

func (h *FileHandler) userLimits(ctx context.Context, userID string) (config.UserLimits, error) {
	plan, err := h.metadataStore.GetUserPlan(ctx, userID)
	if err != nil {
		return config.UserLimits{}, err
	}
	return h.limits.For(plan)
}

func fileObjectKey(ownerID, fileID string) string {
	return path.Join("users", ownerID, "files", fileID)
}

func safeName(value, fallback string) string {
	value = strings.TrimSpace(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
	if value == "" || value == "." || value == ".." {
		return fallback
	}
	return value
}
