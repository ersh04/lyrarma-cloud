package webui

import (
	"github.com/flosch/pongo2/v6"
	"github.com/gofiber/fiber/v3"
)

func (h *Handler) privacyPolicy(c fiber.Ctx) error {
	return h.renderPolicy(c, "privacy", "privacy_policy_title")
}

func (h *Handler) idPolicy(c fiber.Ctx) error {
	return h.renderPolicy(c, "id", "id_policy_title")
}

func (h *Handler) renderPolicy(c fiber.Ctx, kind, titleKey string) error {
	return h.render(c, "policy.html", pongo2.Context{
		"policy_kind":  kind,
		"policy_title": h.message(c, titleKey),
	}, fiber.StatusOK)
}
