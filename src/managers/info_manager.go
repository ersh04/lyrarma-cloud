package managers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/httpresponse"
)

// InfoHandler handles requests related to application information, such as maximum upload size
type InfoHandler struct {
	cfg *config.Config
}

// NewInfoHandler creates a new instance of InfoHandler
func NewInfoHandler(cfg *config.Config) *InfoHandler {
	return &InfoHandler{
		cfg: cfg,
	}
}

// MaxUploadSizeHandler handles the request to get the maximum upload size
func (h *InfoHandler) MaxUploadSizeHandler(c fiber.Ctx) {
	httpresponse.WriteJSON(c, fiber.StatusOK, map[string]int{"max_upload_size": int(h.cfg.Server.MaxUploadSize)})
}
