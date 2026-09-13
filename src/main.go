package main

import (
	"log"
	"log/slog"
	"path/filepath"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
	cloudapi "github.com/lyrarma/cloud-api/src/api"
	cloudconfig "github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/webui"
)

// newRouter creates the root Fiber application and mounts the cloud storage routes.
func newRouter(cfg cloudconfig.Config, application *fiber.App) *fiber.App {
	serverConfig := cfg.Server()
	webConfig := cfg.Web()
	app := fiber.New(fiber.Config{
		ReadTimeout:                  serverConfig.ReadTimeout(),
		WriteTimeout:                 serverConfig.WriteTimeout(),
		BodyLimit:                    cfg.UserLimits().RequestBodyLimit(),
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
	})

	app.Get("/static*", static.New(webConfig.StaticDir()))
	app.Get("/icons*", static.New(webConfig.IconsDir()))
	app.Get("/service-worker.js", func(c fiber.Ctx) error {
		return c.SendFile(filepath.Join(webConfig.StaticDir(), "service-worker.js"))
	})
	app.Get("/manifest.json", func(c fiber.Ctx) error {
		return c.SendFile(filepath.Join(webConfig.StaticDir(), "manifest.json"))
	})
	app.Get("/favicon.ico", func(c fiber.Ctx) error {
		return c.SendFile(filepath.Join(webConfig.IconsDir(), "favicon.ico"))
	})
	app.Use("/", application)

	return app
}

// newCloudApplication initializes the cloud storage API and web interface.
func newCloudApplication(cfg cloudconfig.Config, logger *slog.Logger) (*cloudapi.API, *fiber.App, error) {
	cloudAPI, err := cloudapi.New(cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	webConfig := cfg.Web()
	sessionConfig := cfg.Session()
	jwtConfig := cfg.JWT()
	_, err = webui.New(cloudAPI.App(), webui.Config{
		TemplatesDir:     webConfig.TemplatesDir(),
		TranslationsFile: webConfig.TranslationsFile(),
		AuthCookieName:   sessionConfig.CookieName(),
		CookiePath:       sessionConfig.CookiePath(),
		CookieHTTPOnly:   sessionConfig.CookieHTTPOnly(),
		CookieSecure:     sessionConfig.CookieSecure(),
		CookieSameSite:   sessionConfig.CookieSameSite(),
		TokenTTL:         jwtConfig.TokenTTL(),
		BetaTestKey:      cfg.BetaTestKey(),
		MaxUploadSize:    cfg.UserLimits().HardMaxFileBytes(),
	}, logger)
	if err != nil {
		cloudAPI.Close()
		return nil, nil, err
	}

	return cloudAPI, newRouter(cfg, cloudAPI.App()), nil
}

func main() {
	cfg, err := cloudconfig.Load()
	if err != nil {
		log.Fatal(err)
	}

	cloudAPI, app, err := newCloudApplication(cfg, slog.Default())
	if err != nil {
		log.Fatal(err)
	}
	defer cloudAPI.Close()

	if err := app.Listen(cfg.Server().Address()); err != nil {
		log.Fatal(err)
	}
}
