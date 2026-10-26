package webui

import (
	"fmt"
	"math"
	"strings"

	"github.com/gofiber/fiber/v3"
)

type dashboardLimits struct {
	Plan                []string `json:"plan"`
	MaxUploadSize       int64    `json:"max_upload_size"`
	MaxStorageSize      int64    `json:"max_storage_size"`
	UsedStorageSize     int64    `json:"used_storage_size"`
	ReservedStorageSize int64    `json:"reserved_storage_size"`
}

func storageContext(limits *dashboardLimits, language string) map[string]any {
	if limits == nil || (limits.MaxStorageSize <= 0 && limits.MaxStorageSize != -1) || limits.UsedStorageSize < 0 || limits.ReservedStorageSize < 0 {
		return map[string]any{"available": false, "percent": "0", "state": "normal"}
	}
	used := float64(limits.UsedStorageSize) + float64(limits.ReservedStorageSize)
	unlimited := limits.MaxStorageSize == -1
	percent := 0.0
	limitGB := ""
	if !unlimited {
		percent = math.Min(100, used/float64(limits.MaxStorageSize)*100)
		limitGB = formatStorageGB(float64(limits.MaxStorageSize), language)
	}
	state := "normal"
	if percent >= 100 {
		state = "full"
	} else if percent >= 80 {
		state = "warning"
	}
	return map[string]any{
		"available":    true,
		"unlimited":    unlimited,
		"used_gb":      formatStorageGB(used, language),
		"limit_gb":     limitGB,
		"reserved_gb":  formatStorageGB(float64(limits.ReservedStorageSize), language),
		"has_reserved": limits.ReservedStorageSize > 0,
		"percent":      fmt.Sprintf("%.2f", percent),
		"state":        state,
		"plan":         strings.Join(limits.Plan, ", "),
	}
}

func formatStorageGB(bytes float64, language string) string {
	gigabytes := bytes / (1024 * 1024 * 1024)
	prefix := ""
	if gigabytes > 0 && gigabytes < 0.01 {
		prefix = "<"
		gigabytes = 0.01
	}
	formatted := fmt.Sprintf("%.2f", gigabytes)
	if language == "ru" {
		formatted = strings.ReplaceAll(formatted, ".", ",")
	}
	return prefix + formatted
}

func (h *Handler) storageUsage(c fiber.Ctx) error {
	err := h.proxyAPI(c, fiber.MethodGet, "/api/info/limits", h.token(c))
	c.Set(fiber.HeaderCacheControl, "no-store")
	return err
}
