// Package api assembles the cloud storage HTTP API.
package api

import (
	"context"
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

// New creates the API, connects PostgreSQL and S3, and registers the routes.
func New(cfg config.Config, logger *slog.Logger) (*API, error) {
	if logger == nil {
		logger = slog.Default()
	}

	storageConfig := cfg.Storage()
	limits := cfg.UserLimits()
	store, err := storage.New(
		storageConfig.DataDir(),
		cfg.DatabaseURL(),
		limits.DefaultPlan(),
		limits.PlanNames(),
	)
	if err != nil {
		return nil, err
	}

	fileStore, err := storage.NewS3Store(context.Background(), storage.S3StoreConfig{
		Bucket:          storageConfig.Bucket(),
		Region:          storageConfig.Region(),
		Endpoint:        storageConfig.Endpoint(),
		AccessKeyID:     storageConfig.AccessKeyID(),
		SecretAccessKey: storageConfig.SecretAccessKey(),
		UsePathStyle:    storageConfig.UsePathStyle(),
	})
	if err != nil {
		store.Close()
		return nil, err
	}

	api := &API{
		store:   store,
		s3Store: fileStore,
		app:     fiber.New(fiber.Config{BodyLimit: limits.RequestBodyLimit()}),
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

// App returns the Fiber application with registered API routes.
func (a *API) App() *fiber.App {
	return a.app
}

// registerRoutes registers authentication, file, folder, and information routes.
func (a *API) registerRoutes(cfg config.Config, logger *slog.Logger) {
	jwtConfig := cfg.JWT()
	limits := cfg.UserLimits()
	authHandler := managers.NewAuthHandler(a.store, jwtConfig, cfg.BetaTestKey(), logger)
	fileHandler := managers.NewFileHandler(a.store, a.s3Store, limits, logger)
	folderHandler := managers.NewFolderHandler(a.store, a.s3Store, logger)
	infoHandler := managers.NewInfoHandler(a.store, limits, logger)
	requireAuth := middleware.RequireAuth(jwtConfig.Secret())

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

	// Information routes.
	api.Get("/info/max_upload_size", infoHandler.MaxUploadSizeHandler)
	api.Get("/info/limits", requireAuth, infoHandler.UserLimitsHandler)
}
