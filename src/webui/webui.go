// Package webui implements the server-rendered cloud storage web interface.
package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/flosch/pongo2/v6"
	"github.com/gofiber/fiber/v3"
)

// Config contains the web interface settings.
type Config struct {
	TemplatesDir     string
	TranslationsFile string
	AuthCookieName   string
	CookiePath       string
	CookieHTTPOnly   bool
	CookieSecure     bool
	CookieSameSite   string
	TokenTTL         time.Duration
	BetaTestKey      string
	MaxUploadSize    int64
}

// Handler serves HTML pages and delegates actions to the internal API.
type Handler struct {
	config       Config
	logger       *slog.Logger
	templates    map[string]*pongo2.Template
	translations map[string]map[string]string
	api          *fiber.App
}

// New loads the templates and registers the web interface routes.
func New(api *fiber.App, config Config, logger *slog.Logger) (*Handler, error) {
	if api == nil {
		return nil, fmt.Errorf("cloud API handler is not configured")
	}
	if strings.TrimSpace(config.BetaTestKey) == "" {
		return nil, fmt.Errorf("beta key is not configured")
	}
	if strings.TrimSpace(config.AuthCookieName) == "" {
		return nil, fmt.Errorf("authentication cookie name is not configured")
	}
	if config.CookiePath == "" {
		config.CookiePath = "/"
	}
	if logger == nil {
		logger = slog.Default()
	}

	pongo2.SetAutoescape(true)
	fileSystem := pongo2.MustNewLocalFileSystemLoader(config.TemplatesDir)
	templateSet := pongo2.NewSet("web", fileSystem)
	templateSet.Debug = false

	templates := make(map[string]*pongo2.Template)
	for _, name := range []string{"login.html", "register.html", "dashboard.html", "shared_file.html", "shared_folder.html"} {
		parsed, err := templateSet.FromFile(name)
		if err != nil {
			return nil, fmt.Errorf("load template %s: %w", name, err)
		}
		templates[name] = parsed
	}

	translationsData, err := os.ReadFile(config.TranslationsFile)
	if err != nil {
		return nil, fmt.Errorf("read translations: %w", err)
	}
	translations := make(map[string]map[string]string)
	if err := json.Unmarshal(translationsData, &translations); err != nil {
		return nil, fmt.Errorf("parse translations: %w", err)
	}

	handler := &Handler{
		api:          api,
		config:       config,
		logger:       logger,
		templates:    templates,
		translations: translations,
	}
	handler.registerRoutes()
	return handler, nil
}

func (h *Handler) registerRoutes() {
	h.api.Get("/", h.root)
	h.api.Get("/login", h.loginPage)
	h.api.Post("/login", h.login)
	h.api.Get("/register", h.registerPage)
	h.api.Post("/register", h.register)
	h.api.Get("/logout", h.requireSession, h.logout)
	h.api.Get("/dashboard", h.requireSession, h.dashboard)
	h.api.Post("/upload", h.requireSession, h.upload)
	h.api.Get("/download/:fileID", h.requireSession, h.download)
	h.api.Get("/public/download/:fileID", h.publicDownload)
	h.api.Get("/public/folders/download/:folderID", h.publicFolderDownload)
	h.api.Get("/shared-file/:fileID", h.sharedFile)
	h.api.Get("/shared-folder/:folderID", h.sharedFolder)
	h.api.Get("/change_permission/:fileID", h.requireSession, h.changeFilePermission)
	h.api.Post("/delete/:fileID", h.requireSession, h.deleteFile)
	h.api.Post("/folders/create", h.requireSession, h.createFolder)
	h.api.Get("/folders/change_permission/:folderID", h.requireSession, h.changeFolderPermission)
	h.api.Post("/folders/delete/:folderID", h.requireSession, h.deleteFolder)
	h.api.Get("/set-language/:lang", h.setLanguage)
}

func (h *Handler) render(c fiber.Ctx, templateName string, values pongo2.Context, status int) error {
	context := h.baseContext(c)
	for key, value := range values {
		context[key] = value
	}

	body, err := h.templates[templateName].ExecuteBytes(context)
	if err != nil {
		h.logger.Error("template rendering failed", "template", templateName, "err", err)
		return c.Status(fiber.StatusInternalServerError).SendString("failed to render the page")
	}

	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.Status(status).Send(body)
}

func (h *Handler) baseContext(c fiber.Ctx) pongo2.Context {
	lang := h.requestLanguage(c)
	return pongo2.Context{
		"lang":          lang,
		"authenticated": h.token(c) != "",
		"username":      h.username(c),
		"current_url":   c.OriginalURL(),
		"language_next": url.QueryEscape(c.OriginalURL()),
		"t": func(key string) string {
			return h.translate(lang, key)
		},
		"filesize":      formatFileSize,
		"filetype_icon": fileTypeIcon,
	}
}

func (h *Handler) requestLanguage(c fiber.Ctx) string {
	return normalizeLanguage(c.Cookies(languageCookieName, defaultLanguage))
}

func (h *Handler) translate(lang, key string) string {
	if value := h.translations[lang][key]; value != "" {
		return value
	}
	if value := h.translations[defaultLanguage][key]; value != "" {
		return value
	}
	return key
}

func (h *Handler) message(c fiber.Ctx, key string) string {
	return h.translate(h.requestLanguage(c), key)
}

func normalizeLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "ru") {
		return "ru"
	}
	return defaultLanguage
}

func formatFileSize(size int64) string {
	value := float64(size)
	for _, unit := range []string{"B", "KB", "MB", "GB"} {
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		value /= 1024
	}
	return fmt.Sprintf("%.1f TB", value)
}

func fileTypeIcon(contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "🖼"
	case strings.HasPrefix(contentType, "video/"):
		return "🎬"
	case strings.HasPrefix(contentType, "audio/"):
		return "🎵"
	case contentType == "application/pdf":
		return "📄"
	case contentType == "application/zip", contentType == "application/x-tar", contentType == "application/gzip":
		return "📦"
	case strings.HasPrefix(contentType, "text/"):
		return "📝"
	default:
		return "📎"
	}
}
