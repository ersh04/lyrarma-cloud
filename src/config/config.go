// Package config contains the cloud API configuration.
package config

import "time"

// Config contains the handler, storage, and JWT settings.
type Config struct {
	Server      ServerConfig
	Storage     StorageConfig
	JWT         JWTConfig
	DatabaseURL string
}

// ServerConfig contains the HTTP request limits.
type ServerConfig struct {
	MaxUploadSize int64
}

// StorageConfig contains the S3 file storage settings.
type StorageConfig struct {
	DataDir         string
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
}

// JWTConfig contains the token signing settings.
type JWTConfig struct {
	Secret   string
	TokenTTL time.Duration
}
