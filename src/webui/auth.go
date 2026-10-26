package webui

import (
	"crypto/subtle"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/flosch/pongo2/v6"
	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
)

func (h *Handler) loginPage(c fiber.Ctx) error {
	if h.token(c) != "" {
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard")
	}
	return h.render(c, "login.html", pongo2.Context{
		"registered":           c.Query("registered") != "",
		"registration_pending": c.Query("registration_pending") != "",
		"expired":              c.Query("expired") != "",
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

func (h *Handler) confirmRegistration(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set("Referrer-Policy", "no-referrer")
	c.Set("X-Robots-Tag", "noindex, nofollow")
	response, err := h.callAPI(c, fiber.MethodGet, "/api/auth/confirm-registration?key="+url.QueryEscape(c.Query("key")), nil, "", "")
	if err != nil {
		return h.render(c, "login.html", pongo2.Context{"error": h.message(c, "error_confirmation_failed")}, fiber.StatusServiceUnavailable)
	}
	if response.status != fiber.StatusOK {
		return h.render(c, "login.html", pongo2.Context{"error": h.apiMessage(c, response, "error_confirmation_failed")}, response.status)
	}
	var confirmation models.RegistrationConfirmationResponse
	if err := json.Unmarshal(response.body, &confirmation); err != nil || confirmation.Token == "" || confirmation.Username == "" || !confirmation.ExpiresAt.After(time.Now()) {
		return h.render(c, "login.html", pongo2.Context{"error": h.message(c, "error_invalid_server_response")}, fiber.StatusBadGateway)
	}
	h.setAuthCookies(c, confirmation.Token, confirmation.Username, confirmation.ExpiresAt)
	return c.Redirect().Status(fiber.StatusFound).To("/dashboard")
}

func (h *Handler) register(c fiber.Ctx) error {
	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	if password != c.FormValue("password_confirm") {
		return h.render(c, "register.html", pongo2.Context{"error": h.message(c, "error_passwords_mismatch")}, fiber.StatusBadRequest)
	}
	if h.config.BetaTestKey != "" && !equalSecret(c.FormValue("beta_key"), h.config.BetaTestKey) {
		return h.render(c, "register.html", pongo2.Context{"error": h.message(c, "error_invalid_beta_key")}, fiber.StatusForbidden)
	}
	privacyAccepted := c.FormValue("privacy_policy_accepted") == "true"
	idAccepted := c.FormValue("id_policy_accepted") == "true"
	if !privacyAccepted || !idAccepted {
		return h.render(c, "register.html", pongo2.Context{"error": h.message(c, "api_error_policies_not_accepted")}, fiber.StatusBadRequest)
	}

	response, err := h.callJSON(c, fiber.MethodPost, "/api/auth/register", models.RegisterRequest{
		Username:              username,
		Password:              password,
		Email:                 strings.TrimSpace(c.FormValue("email")),
		BetaKey:               c.FormValue("beta_key"),
		PrivacyPolicyAccepted: privacyAccepted,
		IDPolicyAccepted:      idAccepted,
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

	return c.Redirect().Status(fiber.StatusFound).To("/login?registration_pending=1")
}

func equalSecret(value, expected string) bool {
	if expected == "" || len(value) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}
