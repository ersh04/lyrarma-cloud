// Package config loads and exposes immutable application configuration
package config

import "time"

// Config contains a fully validated application settings snapshot
type Config struct {
	server       ServerConfig
	web          WebConfig
	session      SessionConfig
	storage      StorageConfig
	jwt          JWTConfig
	scanner      ScannerConfig
	databaseURL  string
	usersDBURL   string
	betaTestKey  string
	userLimitSet UserLimitSet
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	address      string
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// WebConfig contains web interface resource paths
type WebConfig struct {
	staticDir        string
	iconsDir         string
	templatesDir     string
	translationsFile string
}

// SessionConfig contains browser session cookie settings
type SessionConfig struct {
	cookieName     string
	cookiePath     string
	cookieHTTPOnly bool
	cookieSecure   bool
	cookieSameSite string
}

// StorageConfig contains S3 connection settings
type StorageConfig struct {
	dataDir          string
	bucket           string
	quarantineBucket string
	region           string
	endpoint         string
	accessKeyID      string
	secretAccessKey  string
	usePathStyle     bool
}

// JWTConfig contains token signing settings
type JWTConfig struct {
	secret   string
	tokenTTL time.Duration
}

// ScannerConfig contains background file scanning settings
type ScannerConfig struct {
	enabled           bool
	workerCount       int
	pollInterval      time.Duration
	timeout           time.Duration
	fileCommand       string
	antivirusCommand  string
	imageCommand      string
	videoCommand      string
	frameCommand      string
	archiveCommand    string
	moderationCommand string
	maxArchiveDepth   int
	maxArchiveFiles   int
	maxArchiveBytes   int64
	maxImagePixels    int64
	blockedMIMETypes  []string
}

func (c Config) Server() ServerConfig              { return c.server }
func (c Config) Web() WebConfig                    { return c.web }
func (c Config) Session() SessionConfig            { return c.session }
func (c Config) Storage() StorageConfig            { return c.storage }
func (c Config) JWT() JWTConfig                    { return c.jwt }
func (c Config) Scanner() ScannerConfig            { return c.scanner }
func (c Config) DatabaseURL() string               { return c.databaseURL }
func (c Config) UsersDatabaseURL() string          { return c.usersDBURL }
func (c Config) BetaTestKey() string               { return c.betaTestKey }
func (c Config) UserLimits() UserLimitSet          { return c.userLimitSet }
func (c ServerConfig) Address() string             { return c.address }
func (c ServerConfig) ReadTimeout() time.Duration  { return c.readTimeout }
func (c ServerConfig) WriteTimeout() time.Duration { return c.writeTimeout }
func (c WebConfig) StaticDir() string              { return c.staticDir }
func (c WebConfig) IconsDir() string               { return c.iconsDir }
func (c WebConfig) TemplatesDir() string           { return c.templatesDir }
func (c WebConfig) TranslationsFile() string       { return c.translationsFile }
func (c SessionConfig) CookieName() string         { return c.cookieName }
func (c SessionConfig) CookiePath() string         { return c.cookiePath }
func (c SessionConfig) CookieHTTPOnly() bool       { return c.cookieHTTPOnly }
func (c SessionConfig) CookieSecure() bool         { return c.cookieSecure }
func (c SessionConfig) CookieSameSite() string     { return c.cookieSameSite }
func (c StorageConfig) DataDir() string            { return c.dataDir }
func (c StorageConfig) Bucket() string             { return c.bucket }
func (c StorageConfig) QuarantineBucket() string   { return c.quarantineBucket }
func (c StorageConfig) Region() string             { return c.region }
func (c StorageConfig) Endpoint() string           { return c.endpoint }
func (c StorageConfig) AccessKeyID() string        { return c.accessKeyID }
func (c StorageConfig) SecretAccessKey() string    { return c.secretAccessKey }
func (c StorageConfig) UsePathStyle() bool         { return c.usePathStyle }
func (c JWTConfig) Secret() string                 { return c.secret }
func (c JWTConfig) TokenTTL() time.Duration        { return c.tokenTTL }

func (c ScannerConfig) Enabled() bool               { return c.enabled }
func (c ScannerConfig) WorkerCount() int            { return c.workerCount }
func (c ScannerConfig) PollInterval() time.Duration { return c.pollInterval }
func (c ScannerConfig) Timeout() time.Duration      { return c.timeout }
func (c ScannerConfig) FileCommand() string         { return c.fileCommand }
func (c ScannerConfig) AntivirusCommand() string    { return c.antivirusCommand }
func (c ScannerConfig) ImageCommand() string        { return c.imageCommand }
func (c ScannerConfig) VideoCommand() string        { return c.videoCommand }
func (c ScannerConfig) FrameCommand() string        { return c.frameCommand }
func (c ScannerConfig) ArchiveCommand() string      { return c.archiveCommand }
func (c ScannerConfig) ModerationCommand() string   { return c.moderationCommand }
func (c ScannerConfig) MaxArchiveDepth() int        { return c.maxArchiveDepth }
func (c ScannerConfig) MaxArchiveFiles() int        { return c.maxArchiveFiles }
func (c ScannerConfig) MaxArchiveBytes() int64      { return c.maxArchiveBytes }
func (c ScannerConfig) MaxImagePixels() int64       { return c.maxImagePixels }
func (c ScannerConfig) BlockedMIMETypes() []string {
	return append([]string(nil), c.blockedMIMETypes...)
}
