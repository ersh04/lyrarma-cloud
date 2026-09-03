package envloader

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	serverAddressEnv         = "SERVER_ADDRESS"
	serverReadTimeoutEnv     = "SERVER_READ_TIMEOUT"
	serverWriteTimeoutEnv    = "SERVER_WRITE_TIMEOUT"
	webStaticDirEnv          = "WEB_STATIC_DIR"
	webIconsDirEnv           = "WEB_ICONS_DIR"
	webTemplatesDirEnv       = "WEB_TEMPLATES_DIR"
	webTranslationsFileEnv   = "WEB_TRANSLATIONS_FILE"
	betaTestKeyEnv           = "BETA_TEST_KEY"
	sessionCookieNameEnv     = "SESSION_COOKIE_NAME"
	sessionCookiePathEnv     = "SESSION_COOKIE_PATH"
	sessionCookieHTTPOnlyEnv = "SESSION_COOKIE_HTTP_ONLY"
	sessionCookieSecureEnv   = "SESSION_COOKIE_SECURE"
	sessionCookieSameSiteEnv = "SESSION_COOKIE_SAME_SITE"
	databaseURLEnv           = "DATABASE_URL"
	cloudDataDirEnv          = "DATA_DIR"
	cloudJWTSecretEnv        = "JWT_SECRET"
	cloudJWTTokenTTLEnv      = "JWT_TTL"
	cloudMaxUploadMBEnv      = "MAX_UPLOAD_MB"
	s3BucketEnv              = "S3_BUCKET"
	s3RegionEnv              = "AWS_REGION"
	s3EndpointEnv            = "S3_ENDPOINT"
	s3AccessKeyIDEnv         = "AWS_ACCESS_KEY_ID"
	s3SecretAccessKeyEnv     = "AWS_SECRET_ACCESS_KEY"
	s3UsePathStyleEnv        = "S3_USE_PATH_STYLE"
	mebibyte                 = int64(1024 * 1024)
	virusScannerAPIURLEnv    = "VIRUS_SCANNER_API_URL"
)

type Config struct {
	ServerAddress         string
	ServerReadTimeout     time.Duration
	ServerWriteTimeout    time.Duration
	WebStaticDir          string
	WebIconsDir           string
	WebTemplatesDir       string
	WebTranslationsFile   string
	BetaTestKey           string
	SessionCookieName     string
	SessionCookiePath     string
	SessionCookieHTTPOnly bool
	SessionCookieSecure   bool
	SessionCookieSameSite string
	VirusScannerAPIURL    string
	DatabaseURL           string
	CloudDataDir          string
	CloudJWTSecret        string
	CloudJWTTokenTTL      time.Duration
	CloudMaxUploadSize    int64
	S3Bucket              string
	S3Region              string
	S3Endpoint            string
	S3AccessKeyID         string
	S3SecretAccessKey     string
	S3UsePathStyle        bool
}

func Default() Config {
	return Config{
		ServerAddress:         ":8080",
		ServerReadTimeout:     30 * time.Second,
		ServerWriteTimeout:    60 * time.Second,
		WebStaticDir:          "web/static",
		WebIconsDir:           "web/icons",
		WebTemplatesDir:       "web/templates",
		WebTranslationsFile:   "web/static/translations.json",
		SessionCookieName:     "auth-session",
		SessionCookiePath:     "/",
		SessionCookieHTTPOnly: true,
		SessionCookieSecure:   false,
		SessionCookieSameSite: "Lax",
		CloudDataDir:          "./data",
		CloudJWTTokenTTL:      24 * time.Hour,
		CloudMaxUploadSize:    1024 * mebibyte,
		S3UsePathStyle:        true,
		VirusScannerAPIURL:    "http://localhost:3311/scan",
	}
}

func Load() (Config, error) {
	values, err := loadDotEnv()
	if err != nil {
		return Config{}, err
	}

	config := Default()
	config.ServerAddress = value(values, serverAddressEnv, config.ServerAddress)
	config.WebStaticDir = value(values, webStaticDirEnv, config.WebStaticDir)
	config.WebIconsDir = value(values, webIconsDirEnv, config.WebIconsDir)
	config.WebTemplatesDir = value(values, webTemplatesDirEnv, config.WebTemplatesDir)
	config.WebTranslationsFile = value(values, webTranslationsFileEnv, config.WebTranslationsFile)
	config.BetaTestKey = value(values, betaTestKeyEnv, config.BetaTestKey)
	config.SessionCookieName = value(values, sessionCookieNameEnv, config.SessionCookieName)
	config.SessionCookiePath = value(values, sessionCookiePathEnv, config.SessionCookiePath)
	config.DatabaseURL = value(values, databaseURLEnv, config.DatabaseURL)
	config.CloudDataDir = value(values, cloudDataDirEnv, config.CloudDataDir)
	config.CloudJWTSecret = value(values, cloudJWTSecretEnv, config.CloudJWTSecret)
	config.S3Bucket = value(values, s3BucketEnv, config.S3Bucket)
	config.S3Region = value(values, s3RegionEnv, config.S3Region)
	config.S3Endpoint = value(values, s3EndpointEnv, config.S3Endpoint)
	config.S3AccessKeyID = value(values, s3AccessKeyIDEnv, config.S3AccessKeyID)
	config.S3SecretAccessKey = value(values, s3SecretAccessKeyEnv, config.S3SecretAccessKey)
	config.VirusScannerAPIURL = value(values, virusScannerAPIURLEnv, config.VirusScannerAPIURL)

	if config.ServerReadTimeout, err = positiveDurationValue(values, serverReadTimeoutEnv, config.ServerReadTimeout); err != nil {
		return Config{}, err
	}
	if config.ServerWriteTimeout, err = positiveDurationValue(values, serverWriteTimeoutEnv, config.ServerWriteTimeout); err != nil {
		return Config{}, err
	}
	if config.SessionCookieSameSite, err = sameSiteValue(values, sessionCookieSameSiteEnv, config.SessionCookieSameSite); err != nil {
		return Config{}, err
	}
	if config.SessionCookieHTTPOnly, err = boolValue(values, sessionCookieHTTPOnlyEnv, config.SessionCookieHTTPOnly); err != nil {
		return Config{}, err
	}
	if config.SessionCookieSecure, err = boolValue(values, sessionCookieSecureEnv, config.SessionCookieSecure); err != nil {
		return Config{}, err
	}
	if config.S3UsePathStyle, err = boolValue(values, s3UsePathStyleEnv, config.S3UsePathStyle); err != nil {
		return Config{}, err
	}
	if config.CloudJWTTokenTTL, err = positiveDurationValue(values, cloudJWTTokenTTLEnv, config.CloudJWTTokenTTL); err != nil {
		return Config{}, err
	}

	maxUploadMB, err := positiveInt64Value(values, cloudMaxUploadMBEnv, config.CloudMaxUploadSize/mebibyte)
	if err != nil {
		return Config{}, err
	}
	if maxUploadMB > (1<<63-1-mebibyte)/mebibyte {
		return Config{}, fmt.Errorf("%s contains a value that is too large", cloudMaxUploadMBEnv)
	}
	config.CloudMaxUploadSize = maxUploadMB * mebibyte

	if err := validateConfig(config); err != nil {
		return Config{}, err
	}

	return config, nil
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.DatabaseURL) == "" {
		return fmt.Errorf("%s is required", databaseURLEnv)
	}
	if strings.TrimSpace(config.BetaTestKey) == "" {
		return fmt.Errorf("%s is required", betaTestKeyEnv)
	}
	if strings.TrimSpace(config.CloudJWTSecret) == "" {
		return fmt.Errorf("%s is required", cloudJWTSecretEnv)
	}
	if strings.TrimSpace(config.S3Bucket) == "" {
		return fmt.Errorf("%s is required", s3BucketEnv)
	}
	if strings.TrimSpace(config.S3Region) == "" {
		return fmt.Errorf("%s is required", s3RegionEnv)
	}
	if (strings.TrimSpace(config.S3AccessKeyID) == "") != (strings.TrimSpace(config.S3SecretAccessKey) == "") {
		return fmt.Errorf("%s and %s must be set together", s3AccessKeyIDEnv, s3SecretAccessKeyEnv)
	}
	return nil
}

func loadDotEnv() (map[string]string, error) {
	for _, path := range []string{".env", "../.env", "example.env"} {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}

		values := make(map[string]string)
		for lineNumber, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			name, rawValue, found := strings.Cut(line, "=")
			if !found {
				return nil, fmt.Errorf("%s:%d: expected a variable name followed by =", path, lineNumber+1)
			}

			name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "export "))
			rawValue = strings.TrimSpace(rawValue)
			if unquoted, unquoteErr := strconv.Unquote(rawValue); unquoteErr == nil {
				rawValue = unquoted
			}
			values[name] = rawValue
		}
		return values, nil
	}

	return map[string]string{}, nil
}

func value(values map[string]string, name, fallback string) string {
	if envValue, found := os.LookupEnv(name); found && strings.TrimSpace(envValue) != "" {
		return strings.TrimSpace(envValue)
	}
	if fileValue := strings.TrimSpace(values[name]); fileValue != "" {
		return fileValue
	}
	return fallback
}

func boolValue(values map[string]string, name string, fallback bool) (bool, error) {
	rawValue := value(values, name, strconv.FormatBool(fallback))
	parsed, err := strconv.ParseBool(rawValue)
	if err != nil {
		return false, fmt.Errorf("%s must contain true or false: %w", name, err)
	}
	return parsed, nil
}

func positiveDurationValue(values map[string]string, name string, fallback time.Duration) (time.Duration, error) {
	rawValue := value(values, name, fallback.String())
	parsed, err := time.ParseDuration(rawValue)
	if err != nil {
		return 0, fmt.Errorf("%s contains an invalid duration: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return parsed, nil
}

func positiveInt64Value(values map[string]string, name string, fallback int64) (int64, error) {
	rawValue := value(values, name, strconv.FormatInt(fallback, 10))
	parsed, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must contain an integer: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return parsed, nil
}

func sameSiteValue(values map[string]string, name, fallback string) (string, error) {
	rawValue := value(values, name, fallback)
	switch strings.ToLower(rawValue) {
	case "lax":
		return "Lax", nil
	case "strict":
		return "Strict", nil
	case "none":
		return "None", nil
	default:
		return "", fmt.Errorf("%s must contain Lax, Strict, or None", name)
	}
}
