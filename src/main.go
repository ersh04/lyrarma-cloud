package main

import (
	"log"
	"log/slog"
	"path/filepath"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
	cloudapi "github.com/lyrarma/cloud-api/src/api"
	cloudconfig "github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/envloader"
	"github.com/lyrarma/cloud-api/src/webui"
)

// newRouter creates the root Fiber application and mounts the cloud storage routes.
func newRouter(config envloader.Config, application *fiber.App) *fiber.App {
	app := fiber.New(fiber.Config{
		ReadTimeout:                  config.ServerReadTimeout,
		WriteTimeout:                 config.ServerWriteTimeout,
		BodyLimit:                    int(config.CloudMaxUploadSize + (1 << 20)),
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
	})

	app.Get("/static*", static.New(config.WebStaticDir))
	app.Get("/icons*", static.New(config.WebIconsDir))
	app.Get("/service-worker.js", func(c fiber.Ctx) error {
		return c.SendFile(filepath.Join(config.WebStaticDir, "service-worker.js"))
	})
	app.Get("/manifest.json", func(c fiber.Ctx) error {
		return c.SendFile(filepath.Join(config.WebStaticDir, "manifest.json"))
	})
	app.Get("/favicon.ico", func(c fiber.Ctx) error {
		return c.SendFile(filepath.Join(config.WebIconsDir, "favicon.ico"))
	})
	app.Use("/", application)

	return app
}

// newCloudApplication initializes the cloud storage API and web interface.
func newCloudApplication(config envloader.Config, logger *slog.Logger) (*cloudapi.API, *fiber.App, error) {
	cloudAPI, err := cloudapi.New(&cloudconfig.Config{
		Server: cloudconfig.ServerConfig{
			MaxUploadSize: config.CloudMaxUploadSize,
		},
		Storage: cloudconfig.StorageConfig{
			DataDir:         config.CloudDataDir,
			Bucket:          config.S3Bucket,
			Region:          config.S3Region,
			Endpoint:        config.S3Endpoint,
			AccessKeyID:     config.S3AccessKeyID,
			SecretAccessKey: config.S3SecretAccessKey,
			UsePathStyle:    config.S3UsePathStyle,
		},
		JWT: cloudconfig.JWTConfig{
			Secret:   config.CloudJWTSecret,
			TokenTTL: config.CloudJWTTokenTTL,
		},
		DatabaseURL: config.DatabaseURL,
	}, logger)
	if err != nil {
		return nil, nil, err
	}

	_, err = webui.New(cloudAPI.App(), webui.Config{
		TemplatesDir:     config.WebTemplatesDir,
		TranslationsFile: config.WebTranslationsFile,
		AuthCookieName:   config.SessionCookieName,
		CookiePath:       config.SessionCookiePath,
		CookieHTTPOnly:   config.SessionCookieHTTPOnly,
		CookieSecure:     config.SessionCookieSecure,
		CookieSameSite:   config.SessionCookieSameSite,
		TokenTTL:         config.CloudJWTTokenTTL,
		BetaTestKey:      config.BetaTestKey,
		MaxUploadSize:    config.CloudMaxUploadSize,
	}, logger)
	if err != nil {
		cloudAPI.Close()
		return nil, nil, err
	}

	return cloudAPI, newRouter(config, cloudAPI.App()), nil
}

func main() {
	config, err := envloader.Load()
	if err != nil {
		log.Fatal(err)
	}

	cloudAPI, app, err := newCloudApplication(config, slog.Default())
	if err != nil {
		log.Fatal(err)
	}
	defer cloudAPI.Close()

	if err := app.Listen(config.ServerAddress); err != nil {
		log.Fatal(err)
	}
}
