package webui

import (
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

const (
	defaultLanguage      = "en"
	languageCookieName   = "lyrarma_lang"
	usernameCookieSuffix = "-username"
)

func (h *Handler) root(c fiber.Ctx) error {
	if h.token(c) != "" {
		return c.Redirect().Status(fiber.StatusFound).To("/dashboard")
	}
	return c.Redirect().Status(fiber.StatusFound).To("/login")
}

func (h *Handler) token(c fiber.Ctx) string {
	cookie := c.Cookies(h.config.AuthCookieName)
	if cookie == "" {
		return ""
	}
	return strings.TrimSpace(cookie)
}

func (h *Handler) username(c fiber.Ctx) string {
	cookie := c.Cookies(h.config.AuthCookieName + usernameCookieSuffix)
	if cookie == "" {
		return ""
	}
	return cookie
}

func (h *Handler) setAuthCookies(c fiber.Ctx, token, username string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = int(h.config.TokenTTL.Seconds())
	}

	h.setCookie(c, h.config.AuthCookieName, token, expiresAt, maxAge)
	h.setCookie(c, h.config.AuthCookieName+usernameCookieSuffix, username, expiresAt, maxAge)
}

func (h *Handler) clearAuthCookies(c fiber.Ctx) {
	expiresAt := time.Unix(1, 0)
	h.setCookie(c, h.config.AuthCookieName, "", expiresAt, -1)
	h.setCookie(c, h.config.AuthCookieName+usernameCookieSuffix, "", expiresAt, -1)
}

func (h *Handler) setCookie(c fiber.Ctx, name, value string, expires time.Time, maxAge int) {

	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    value,
		Expires:  expires,
		HTTPOnly: h.config.CookieHTTPOnly,
		Secure:   h.config.CookieSecure,
		SameSite: h.config.CookieSameSite,
		Path:     h.config.CookiePath,
		MaxAge:   maxAge,
	})
}

func (h *Handler) logout(c fiber.Ctx) error {
	h.clearAuthCookies(c)
	return c.Redirect().Status(fiber.StatusFound).To("/login")
}

func (h *Handler) requireSession(c fiber.Ctx) error {
	if h.token(c) == "" {
		return c.Redirect().Status(fiber.StatusFound).To("/login")
	}
	return c.Next()
}

func (h *Handler) setLanguage(c fiber.Ctx) error {
	lang := normalizeLanguage(c.Params("lang"))
	next := safeRedirectPath(c.Query("next"))

	cookie := &fiber.Cookie{
		Name:     languageCookieName,
		Value:    lang,
		Path:     h.config.CookiePath,
		MaxAge:   365 * 24 * 60 * 60,
		Expires:  time.Now().Add(365 * 24 * time.Hour),
		SameSite: fiber.CookieSameSiteLaxMode,
		Secure:   h.config.CookieSecure,
	}
	if strings.HasSuffix(strings.ToLower(c.Hostname()), "lyrarma.com") {
		cookie.Domain = ".lyrarma.com"
	}
	c.Cookie(cookie)
	return c.Redirect().Status(fiber.StatusFound).To(next)
}

func safeRedirectPath(value string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "/"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "/"
	}
	return parsed.RequestURI()
}
