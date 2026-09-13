package managers

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/httpresponse"
	"github.com/lyrarma/cloud-api/src/middleware"
	"github.com/lyrarma/cloud-api/src/storage"
)

// InfoHandler returns system-wide and user-specific limits.
type InfoHandler struct {
	store  *storage.SQLStore
	limits config.UserLimitSet
	logger *slog.Logger
}

type userLimitsResponse struct {
	Level               string `json:"level"`
	MaxUploadSize       int64  `json:"max_upload_size"`
	MaxStorageSize      int64  `json:"max_storage_size"`
	UsedStorageSize     int64  `json:"used_storage_size"`
	ReservedStorageSize int64  `json:"reserved_storage_size"`
}

// NewInfoHandler creates a handler for limit information.
func NewInfoHandler(store *storage.SQLStore, limits config.UserLimitSet, logger *slog.Logger) *InfoHandler {
	return &InfoHandler{store: store, limits: limits, logger: logger}
}

// MaxUploadSizeHandler returns the absolute system file-size limit.
func (h *InfoHandler) MaxUploadSizeHandler(c fiber.Ctx) {
	httpresponse.WriteJSON(c, fiber.StatusOK, map[string]int64{
		"max_upload_size": h.limits.HardMaxFileBytes(),
	})
}

// UserLimitsHandler returns the authenticated user's current limits.
func (h *InfoHandler) UserLimitsHandler(c fiber.Ctx) {
	userID := middleware.UserIDFromContext(c.Context())
	plan, err := h.store.GetUserPlan(c.Context(), userID)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			httpresponse.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "user no longer exists")
			return
		}
		h.logger.Error("failed to retrieve user plan", "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to retrieve user limits")
		return
	}

	limits, err := h.limits.For(plan)
	if err != nil {
		h.logger.Error("user limits are not configured", "user_id", userID, "plan", plan, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to retrieve user limits")
		return
	}

	usedBytes, reservedBytes, err := h.store.StorageUsage(c.Context(), userID)
	if err != nil {
		h.logger.Error("failed to retrieve storage usage", "user_id", userID, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "failed to retrieve storage usage")
		return
	}

	httpresponse.WriteJSON(c, fiber.StatusOK, userLimitsResponse{
		Level:               plan,
		MaxUploadSize:       limits.MaxFileBytes(),
		MaxStorageSize:      limits.MaxStorageBytes(),
		UsedStorageSize:     usedBytes,
		ReservedStorageSize: reservedBytes,
	})
}
