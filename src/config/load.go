package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	configFileEnv                  = "CONFIG_FILE"
	serverAddressEnv               = "SERVER_ADDRESS"
	serverReadTimeoutEnv           = "SERVER_READ_TIMEOUT"
	serverWriteTimeoutEnv          = "SERVER_WRITE_TIMEOUT"
	webStaticDirEnv                = "WEB_STATIC_DIR"
	webIconsDirEnv                 = "WEB_ICONS_DIR"
	webTemplatesDirEnv             = "WEB_TEMPLATES_DIR"
	webTranslationsFileEnv         = "WEB_TRANSLATIONS_FILE"
	betaTestKeyEnv                 = "BETA_TEST_KEY"
	postControllerBaseURLEnv       = "POST_CONTROLLER_BASE_URL"
	postControllerTokenEnv         = "POST_CONTROLLER_TOKEN"
	registrationConfirmationURLEnv = "REGISTRATION_CONFIRMATION_URL"
	sessionCookieNameEnv           = "SESSION_COOKIE_NAME"
	sessionCookiePathEnv           = "SESSION_COOKIE_PATH"
	sessionCookieHTTPOnlyEnv       = "SESSION_COOKIE_HTTP_ONLY"
	sessionCookieSecureEnv         = "SESSION_COOKIE_SECURE"
	sessionCookieSameSiteEnv       = "SESSION_COOKIE_SAME_SITE"
	databaseURLEnv                 = "DATABASE_URL"
	usersDatabaseURLEnv            = "USERS_DATABASE_URL"
	cloudDataDirEnv                = "DATA_DIR"
	cloudJWTSecretEnv              = "JWT_SECRET"
	cloudJWTTokenTTLEnv            = "JWT_TTL"
	hardMaxUploadMBEnv             = "MAX_UPLOAD_MB"
	hardMaxStorageMBEnv            = "MAX_STORAGE_MB"
	defaultUserPlanEnv             = "DEFAULT_USER_PLAN"
	userPlansJSONEnv               = "USER_PLANS_JSON"
	s3BucketEnv                    = "S3_BUCKET"
	s3QuarantineBucketEnv          = "S3_QUARANTINE_BUCKET"
	s3RegionEnv                    = "AWS_REGION"
	s3EndpointEnv                  = "S3_ENDPOINT"
	s3AccessKeyIDEnv               = "AWS_ACCESS_KEY_ID"
	s3SecretAccessKeyEnv           = "AWS_SECRET_ACCESS_KEY"
	s3UsePathStyleEnv              = "S3_USE_PATH_STYLE"
	scannerEnabledEnv              = "SCANNER_ENABLED"
	scannerWorkersEnv              = "SCANNER_WORKERS"
	scannerPollIntervalEnv         = "SCANNER_POLL_INTERVAL"
	scannerTimeoutEnv              = "SCANNER_TIMEOUT"
	scannerFileCommandEnv          = "SCANNER_FILE_COMMAND"
	scannerAntivirusEnv            = "SCANNER_ANTIVIRUS_COMMAND"
	scannerImageCommandEnv         = "SCANNER_IMAGE_COMMAND"
	scannerVideoCommandEnv         = "SCANNER_VIDEO_COMMAND"
	scannerFrameCommandEnv         = "SCANNER_FRAME_COMMAND"
	scannerArchiveCommandEnv       = "SCANNER_ARCHIVE_COMMAND"
	scannerModerationEnv           = "SCANNER_MODERATION_COMMAND"
	scannerArchiveDepthEnv         = "SCANNER_MAX_ARCHIVE_DEPTH"
	scannerArchiveFilesEnv         = "SCANNER_MAX_ARCHIVE_FILES"
	scannerArchiveSizeMBEnv        = "SCANNER_MAX_ARCHIVE_MB"
	scannerMaxImagePixelsEnv       = "SCANNER_MAX_IMAGE_PIXELS"
	scannerBlockedMIMEsEnv         = "SCANNER_BLOCKED_MIME_TYPES"
	mebibyte                       = int64(1024 * 1024)
)

const (
	exampleJWTSecret   = "local-dev-jwt-secret-change-before-production"
	exampleBetaTestKey = "local-dev-beta-test-key-change-before-production"
)

type planJSONConfig struct {
	Name         string `json:"name"`
	MaxUploadMB  int64  `json:"max_upload_mb"`
	MaxStorageMB int64  `json:"max_storage_mb"`
}

// Load reads, validates, and returns one immutable configuration snapshot
func Load() (Config, error) {
	values, err := loadDotEnv()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		server: ServerConfig{
			address:      value(values, serverAddressEnv, ":8080"),
			readTimeout:  30 * time.Second,
			writeTimeout: 60 * time.Second,
		},
		web: WebConfig{
			staticDir:        value(values, webStaticDirEnv, "web/static"),
			iconsDir:         value(values, webIconsDirEnv, "web/icons"),
			templatesDir:     value(values, webTemplatesDirEnv, "web/templates"),
			translationsFile: value(values, webTranslationsFileEnv, "web/static/translations.json"),
		},
		session: SessionConfig{
			cookieName:     value(values, sessionCookieNameEnv, "auth-session"),
			cookiePath:     value(values, sessionCookiePathEnv, "/"),
			cookieHTTPOnly: true,
			cookieSecure:   false,
			cookieSameSite: "Lax",
		},
		storage: StorageConfig{
			dataDir:          value(values, cloudDataDirEnv, "./data"),
			bucket:           value(values, s3BucketEnv, ""),
			quarantineBucket: value(values, s3QuarantineBucketEnv, ""),
			region:           value(values, s3RegionEnv, ""),
			endpoint:         value(values, s3EndpointEnv, ""),
			accessKeyID:      value(values, s3AccessKeyIDEnv, ""),
			secretAccessKey:  value(values, s3SecretAccessKeyEnv, ""),
			usePathStyle:     true,
		},
		jwt: JWTConfig{
			secret:   value(values, cloudJWTSecretEnv, ""),
			tokenTTL: 24 * time.Hour,
		},
		scanner: ScannerConfig{
			enabled:           true,
			workerCount:       2,
			pollInterval:      2 * time.Second,
			timeout:           10 * time.Minute,
			fileCommand:       value(values, scannerFileCommandEnv, "file"),
			antivirusCommand:  value(values, scannerAntivirusEnv, "clamdscan"),
			imageCommand:      value(values, scannerImageCommandEnv, "identify"),
			videoCommand:      value(values, scannerVideoCommandEnv, "ffprobe"),
			frameCommand:      value(values, scannerFrameCommandEnv, "ffmpeg"),
			archiveCommand:    value(values, scannerArchiveCommandEnv, "7z"),
			moderationCommand: value(values, scannerModerationEnv, ""),
			maxArchiveDepth:   5,
			maxArchiveFiles:   10000,
			maxArchiveBytes:   5 * 1024 * 1024 * 1024,
			maxImagePixels:    100_000_000,
			blockedMIMETypes: []string{
				"application/x-dosexec",
				"application/x-executable",
				"application/x-elf",
				"application/x-mach-binary",
				"application/x-object",
				"application/x-pie-executable",
				"application/x-sharedlib",
				"application/x-msdownload",
				"application/vnd.microsoft.portable-executable",
			},
		},
		databaseURL:                 value(values, databaseURLEnv, ""),
		usersDBURL:                  value(values, usersDatabaseURLEnv, ""),
		betaTestKey:                 value(values, betaTestKeyEnv, ""),
		postControllerURL:           value(values, postControllerBaseURLEnv, ""),
		postControllerToken:         value(values, postControllerTokenEnv, ""),
		registrationConfirmationURL: value(values, registrationConfirmationURLEnv, ""),
	}

	if cfg.server.readTimeout, err = positiveDurationValue(values, serverReadTimeoutEnv, cfg.server.readTimeout); err != nil {
		return Config{}, err
	}
	if cfg.server.writeTimeout, err = positiveDurationValue(values, serverWriteTimeoutEnv, cfg.server.writeTimeout); err != nil {
		return Config{}, err
	}
	if cfg.session.cookieSameSite, err = sameSiteValue(values, sessionCookieSameSiteEnv, cfg.session.cookieSameSite); err != nil {
		return Config{}, err
	}
	if cfg.session.cookieHTTPOnly, err = boolValue(values, sessionCookieHTTPOnlyEnv, cfg.session.cookieHTTPOnly); err != nil {
		return Config{}, err
	}
	if cfg.session.cookieSecure, err = boolValue(values, sessionCookieSecureEnv, cfg.session.cookieSecure); err != nil {
		return Config{}, err
	}
	if cfg.storage.usePathStyle, err = boolValue(values, s3UsePathStyleEnv, cfg.storage.usePathStyle); err != nil {
		return Config{}, err
	}
	if cfg.jwt.tokenTTL, err = positiveDurationValue(values, cloudJWTTokenTTLEnv, cfg.jwt.tokenTTL); err != nil {
		return Config{}, err
	}
	if cfg.scanner.enabled, err = boolValue(values, scannerEnabledEnv, cfg.scanner.enabled); err != nil {
		return Config{}, err
	}
	if cfg.scanner.workerCount, err = positiveIntegerValue(values, scannerWorkersEnv, cfg.scanner.workerCount); err != nil {
		return Config{}, err
	}
	if cfg.scanner.pollInterval, err = positiveDurationValue(values, scannerPollIntervalEnv, cfg.scanner.pollInterval); err != nil {
		return Config{}, err
	}
	if cfg.scanner.timeout, err = positiveDurationValue(values, scannerTimeoutEnv, cfg.scanner.timeout); err != nil {
		return Config{}, err
	}
	if cfg.scanner.maxArchiveDepth, err = positiveIntegerValue(values, scannerArchiveDepthEnv, cfg.scanner.maxArchiveDepth); err != nil {
		return Config{}, err
	}
	if cfg.scanner.maxArchiveFiles, err = positiveIntegerValue(values, scannerArchiveFilesEnv, cfg.scanner.maxArchiveFiles); err != nil {
		return Config{}, err
	}
	if cfg.scanner.maxArchiveBytes, err = positiveMebibytesValue(values, scannerArchiveSizeMBEnv, cfg.scanner.maxArchiveBytes); err != nil {
		return Config{}, err
	}
	if cfg.scanner.maxImagePixels, err = positiveInt64Value(values, scannerMaxImagePixelsEnv, cfg.scanner.maxImagePixels); err != nil {
		return Config{}, err
	}
	// An explicitly empty value disables the MIME denylist; an absent setting preserves the built-in list
	rawBlockedMIMEs, blockedMIMEsConfigured := os.LookupEnv(scannerBlockedMIMEsEnv)
	if !blockedMIMEsConfigured {
		rawBlockedMIMEs, blockedMIMEsConfigured = values[scannerBlockedMIMEsEnv]
	}
	if blockedMIMEsConfigured {
		cfg.scanner.blockedMIMETypes = commaSeparatedValues(rawBlockedMIMEs)
	}

	hardMaxFile, err := requiredMebibytesValue(values, hardMaxUploadMBEnv)
	if err != nil {
		return Config{}, err
	}
	hardMaxStorage, err := requiredMebibytesValue(values, hardMaxStorageMBEnv)
	if err != nil {
		return Config{}, err
	}
	defaultPlan, plans, err := loadUserPlans(values)
	if err != nil {
		return Config{}, err
	}
	cfg.userLimitSet, err = NewUserLimitSet(
		hardMaxStorage,
		hardMaxFile,
		defaultPlan,
		plans,
	)
	if err != nil {
		return Config{}, fmt.Errorf("invalid limit configuration: %w", err)
	}

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadUserPlans(values map[string]string) (string, map[string]UserLimits, error) {
	defaultPlan := value(values, defaultUserPlanEnv, "")
	if defaultPlan == "" {
		return "", nil, fmt.Errorf("%s is required", defaultUserPlanEnv)
	}
	if _, err := ParsePlanName(defaultPlan); err != nil {
		return "", nil, fmt.Errorf("%s is invalid: %w", defaultUserPlanEnv, err)
	}

	rawPlans := value(values, userPlansJSONEnv, "")
	if rawPlans == "" {
		return "", nil, fmt.Errorf("%s is required", userPlansJSONEnv)
	}
	decoder := json.NewDecoder(strings.NewReader(rawPlans))
	decoder.DisallowUnknownFields()

	var configuredPlans []planJSONConfig
	if err := decoder.Decode(&configuredPlans); err != nil {
		return "", nil, fmt.Errorf("%s contains invalid JSON: %w", userPlansJSONEnv, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", nil, fmt.Errorf("%s must contain exactly one JSON array", userPlansJSONEnv)
	}
	if len(configuredPlans) == 0 {
		return "", nil, fmt.Errorf("%s must contain at least one service plan", userPlansJSONEnv)
	}

	plans := make(map[string]UserLimits, len(configuredPlans))
	for index, configuredPlan := range configuredPlans {
		name, err := ParsePlanName(configuredPlan.Name)
		if err != nil {
			return "", nil, fmt.Errorf("%s[%d].name is invalid: %w", userPlansJSONEnv, index, err)
		}
		if _, exists := plans[name]; exists {
			return "", nil, fmt.Errorf("%s contains duplicate service plan %q", userPlansJSONEnv, name)
		}
		maxStorage, err := mebibytesValue(
			configuredPlan.MaxStorageMB,
			fmt.Sprintf("%s[%d].max_storage_mb", userPlansJSONEnv, index),
		)
		if err != nil {
			return "", nil, err
		}
		maxFile, err := mebibytesValue(
			configuredPlan.MaxUploadMB,
			fmt.Sprintf("%s[%d].max_upload_mb", userPlansJSONEnv, index),
		)
		if err != nil {
			return "", nil, err
		}
		limits, err := NewUserLimits(maxStorage, maxFile)
		if err != nil {
			return "", nil, fmt.Errorf("limits for service plan %q are invalid: %w", name, err)
		}
		plans[name] = limits
	}

	return defaultPlan, plans, nil
}

func mebibytesValue(parsed int64, name string) (int64, error) {
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if parsed > maxInt64/mebibyte {
		return 0, fmt.Errorf("%s contains a value that is too large", name)
	}
	return parsed * mebibyte, nil
}

func validateConfig(cfg Config) error {
	if cfg.registrationConfirmationURL != "" {
		link, err := url.Parse(cfg.registrationConfirmationURL)
		if err != nil || (link.Scheme != "https" && link.Scheme != "http") || link.Hostname() == "" || link.User != nil || link.Fragment != "" {
			return fmt.Errorf("%s must be an absolute HTTP(S) URL without credentials or a fragment", registrationConfirmationURLEnv)
		}
	}
	if strings.TrimSpace(cfg.databaseURL) == "" {
		return fmt.Errorf("%s is required", databaseURLEnv)
	}
	if strings.TrimSpace(cfg.usersDBURL) == "" {
		return fmt.Errorf("%s is required", usersDatabaseURLEnv)
	}
	if cfg.betaTestKey != "" && (len(cfg.betaTestKey) < 16 || cfg.betaTestKey == exampleBetaTestKey) {
		return fmt.Errorf("%s must contain at least 16 bytes and differ from the example value", betaTestKeyEnv)
	}
	if len(cfg.jwt.secret) < 32 || cfg.jwt.secret == exampleJWTSecret {
		return fmt.Errorf("%s must contain at least 32 bytes and differ from the example value", cloudJWTSecretEnv)
	}
	if strings.TrimSpace(cfg.storage.bucket) == "" {
		return fmt.Errorf("%s is required", s3BucketEnv)
	}
	if strings.TrimSpace(cfg.storage.quarantineBucket) == "" {
		return fmt.Errorf("%s is required", s3QuarantineBucketEnv)
	}
	if cfg.storage.quarantineBucket == cfg.storage.bucket {
		return fmt.Errorf("%s must differ from %s", s3QuarantineBucketEnv, s3BucketEnv)
	}
	if strings.TrimSpace(cfg.storage.region) == "" {
		return fmt.Errorf("%s is required", s3RegionEnv)
	}
	if (strings.TrimSpace(cfg.storage.accessKeyID) == "") != (strings.TrimSpace(cfg.storage.secretAccessKey) == "") {
		return fmt.Errorf("%s and %s must be set together", s3AccessKeyIDEnv, s3SecretAccessKeyEnv)
	}
	if cfg.session.cookieSameSite == "None" && !cfg.session.cookieSecure {
		return fmt.Errorf("%s=None requires %s=true", sessionCookieSameSiteEnv, sessionCookieSecureEnv)
	}
	return nil
}

func loadDotEnv() (map[string]string, error) {
	path, explicit := os.LookupEnv(configFileEnv)
	path = strings.TrimSpace(path)
	if path == "" {
		path = ".env"
		explicit = false
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", path)
	}
	if fileInfo.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s must not be accessible by group or other users", path)
	}
	if fileInfo.Size() > 1<<20 {
		return nil, fmt.Errorf("%s exceeds the 1 MiB configuration file limit", path)
	}

	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("%s exceeds the 1 MiB configuration file limit", path)
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

func requiredMebibytesValue(values map[string]string, name string) (int64, error) {
	rawValue := value(values, name, "")
	if rawValue == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	parsed, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must contain an integer number of MiB: %w", name, err)
	}
	return mebibytesValue(parsed, name)
}

func positiveIntegerValue(values map[string]string, name string, fallback int) (int, error) {
	rawValue := value(values, name, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(rawValue)
	if err != nil {
		return 0, fmt.Errorf("%s must contain an integer: %w", name, err)
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

func positiveMebibytesValue(values map[string]string, name string, fallback int64) (int64, error) {
	rawValue := value(values, name, strconv.FormatInt(fallback/mebibyte, 10))
	parsed, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must contain an integer number of MiB: %w", name, err)
	}
	return mebibytesValue(parsed, name)
}

func commaSeparatedValues(rawValue string) []string {
	values := make([]string, 0)
	seen := make(map[string]struct{})
	for _, item := range strings.Split(rawValue, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		values = append(values, item)
	}
	return values
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
