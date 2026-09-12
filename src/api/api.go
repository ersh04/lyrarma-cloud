// Package api assembles the cloud storage HTTP API.
package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/managers"
	"github.com/lyrarma/cloud-api/src/middleware"
	"github.com/lyrarma/cloud-api/src/storage"
)

// API groups the HTTP routes and data stores.
type API struct {
	store   *storage.SQLStore
	s3Store *storage.S3Store
	app     *fiber.App
}

// New creates the API, connects PostgreSQL and S3, and then registers the routes.
func New(cfg *config.Config, logger *slog.Logger) (*API, error) {
	if cfg == nil {
		return nil, fmt.Errorf("конфигурация API не задана")
	}
	if logger == nil {
		logger = slog.Default()
	}

	store, err := storage.New(cfg.Storage.DataDir, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	fileStore, err := storage.NewS3Store(context.Background(), storage.S3StoreConfig{
		Bucket:          cfg.Storage.Bucket,
		Region:          cfg.Storage.Region,
		Endpoint:        cfg.Storage.Endpoint,
		AccessKeyID:     cfg.Storage.AccessKeyID,
		SecretAccessKey: cfg.Storage.SecretAccessKey,
		UsePathStyle:    cfg.Storage.UsePathStyle,
	})
	if err != nil {
		store.Close()
		return nil, err
	}

	api := &API{
		store:   store,
		s3Store: fileStore,
		app:     fiber.New(fiber.Config{BodyLimit: int(cfg.Server.MaxUploadSize + (1 << 20))}),
	}
	api.registerRoutes(cfg, logger)
	return api, nil
}

// Close closes the PostgreSQL connection.
func (a *API) Close() {
	if a != nil {
		a.store.Close()
	}
}

// App returns the Fiber application with the API routes registered.
func (a *API) App() *fiber.App {
	return a.app
}

// registerRoutes registers the authentication, file, and folder routes.
func (a *API) registerRoutes(cfg *config.Config, logger *slog.Logger) {
	authHandler := managers.NewAuthHandler(a.store, cfg, logger)
	fileHandler := managers.NewFileHandler(a.store, a.s3Store, cfg, logger)
	folderHandler := managers.NewFolderHandler(a.store, a.s3Store, logger)
	infoHandler := managers.NewInfoHandler(cfg)
	requireAuth := middleware.RequireAuth(cfg.JWT.Secret)

	// Service health check route.
	a.app.Get("/health", func(c fiber.Ctx) error {
		c.Set("Content-Type", "application/json")
		return c.Status(fiber.StatusOK).SendString(`{"status":"ok"}`)
	})

	api := a.app.Group("/api")

	// Authentication routes.
	api.Post("/auth/register", authHandler.Register)
	api.Post("/auth/login", authHandler.Login)

	// File management routes.
	api.Get("/files", requireAuth, fileHandler.FileList)
	api.Post("/files/upload", requireAuth, fileHandler.UploadFile)
	api.Get("/files/:fileID/download", requireAuth, fileHandler.DownloadFile)
	api.Get("/files/:fileID/info", requireAuth, fileHandler.FileInfo)
	api.Delete("/files/:fileID", requireAuth, fileHandler.DeleteFile)
	api.Get("/files/:fileID/change_permission/:isPublic",
		requireAuth, fileHandler.ChangeFilePermission)
	api.Get("/public/files/:fileID/download", fileHandler.DownloadPublicFile)
	api.Get("/public/files/:fileID/info", fileHandler.GetPublicFileInfo)

	// Folder management routes.
	api.Get("/folders", requireAuth, folderHandler.List)
	api.Post("/folders/create", requireAuth, folderHandler.Create)
	api.Post("/folders/:folderID/change_permission/:isPublic",
		requireAuth, folderHandler.ChangeFolderPermission)
	api.Delete("/folders/:folderID", requireAuth, folderHandler.Delete)
	api.Get("/public/folders/:folderID/info", folderHandler.GetPublicFolderInfo)
	api.Get("/public/folders/:folderID/download", folderHandler.DownloadPublicFolder)

	// Info routes.
	api.Get("/info/max_upload_size", infoHandler.MaxUploadSizeHandler)
}
