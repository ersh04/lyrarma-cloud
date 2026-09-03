package webui

import (
	"crypto/subtle"
	"encoding/json"
	"strings"

	"github.com/flosch/pongo2/v6"
	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
)

func (h *Handler) loginPage(c fiber.Ctx) error {
	if h.token(c) != "" {
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard")
	}
	return h.render(c, "login.html", pongo2.Context{
		"registered": c.Query("registered") != "",
		"expired":    c.Query("expired") != "",
	}, fiber.StatusOK)
}

func (h *Handler) login(c fiber.Ctx) error {
	username := strings.TrimSpace(c.FormValue("username"))
	response, err := h.callJSON(c, fiber.MethodPost, "/api/auth/login", models.LoginRequest{
		Username: username,
		Password: c.FormValue("password"),
	}, "")
	if err != nil {
		h.logger.Error("internal login request failed", "err", err)
		return h.render(c, "login.html", pongo2.Context{"error": h.message(c, "error_login_failed")}, fiber.StatusServiceUnavailable)
	}
	if response.status != fiber.StatusOK {
		return h.render(c, "login.html", pongo2.Context{
			"error": h.apiMessage(c, response, "error_invalid_credentials"),
		}, response.status)
	}

	var loginResponse models.LoginResponse
	if err := json.Unmarshal(response.body, &loginResponse); err != nil || loginResponse.Token == "" {
		h.logger.Error("invalid login API response", "err", err)
		return h.render(c, "login.html", pongo2.Context{"error": h.message(c, "error_invalid_server_response")}, fiber.StatusBadGateway)
	}

	h.setAuthCookies(c, loginResponse.Token, username, loginResponse.ExpiresAt)
	return c.Redirect().Status(fiber.StatusFound).To("/dashboard")
}

func (h *Handler) registerPage(c fiber.Ctx) error {
	if h.token(c) != "" {
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard")
	}
	return h.render(c, "register.html", nil, fiber.StatusOK)
}

func (h *Handler) register(c fiber.Ctx) error {
	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	if password != c.FormValue("password_confirm") {
		return h.render(c, "register.html", pongo2.Context{"error": h.message(c, "error_passwords_mismatch")}, fiber.StatusBadRequest)
	}
	if !equalSecret(c.FormValue("beta_key"), h.config.BetaTestKey) {
		return h.render(c, "register.html", pongo2.Context{"error": h.message(c, "error_invalid_beta_key")}, fiber.StatusForbidden)
	}

	response, err := h.callJSON(c, fiber.MethodPost, "/api/auth/register", models.RegisterRequest{
		Username: username,
		Password: password,
	}, "")
	if err != nil {
		h.logger.Error("internal registration request failed", "err", err)
		return h.render(c, "register.html", pongo2.Context{"error": h.message(c, "error_register_failed")}, fiber.StatusServiceUnavailable)
	}
	if response.status != fiber.StatusCreated {
		return h.render(c, "register.html", pongo2.Context{
			"error": h.apiMessage(c, response, "error_register_failed"),
		}, response.status)
	}

	return c.Redirect().Status(fiber.StatusFound).To("/login?registered=1")
}

func equalSecret(value, expected string) bool {
	if expected == "" || len(value) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}
