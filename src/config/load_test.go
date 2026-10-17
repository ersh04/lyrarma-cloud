package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBuildsValidatedImmutableLimits(t *testing.T) {
	setValidEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	limits, err := cfg.UserLimits().For("premium")
	if err != nil {
		t.Fatal(err)
	}
	if limits.MaxFileBytes() != 10*1024*1024 {
		t.Fatalf("unexpected premium file limit: %d", limits.MaxFileBytes())
	}
	if cfg.UserLimits().HardMaxFileBytes() != 20*1024*1024 {
		t.Fatalf("unexpected hard file limit: %d", cfg.UserLimits().HardMaxFileBytes())
	}
	if cfg.UserLimits().DefaultPlan() != "user" {
		t.Fatalf("unexpected default plan: %q", cfg.UserLimits().DefaultPlan())
	}
	if cfg.UsersDatabaseURL() != "postgres://localhost/lyrarma-services-users" {
		t.Fatalf("unexpected users database URL: %q", cfg.UsersDatabaseURL())
	}
}

func TestLoadScannerEnabled(t *testing.T) {
	for _, test := range []struct {
		name      string
		envValue  string
		fileValue string
		want      bool
		wantError bool
	}{
		{name: "enabled by default", want: true},
		{name: "explicitly enabled", envValue: "true", want: true},
		{name: "disabled by environment", envValue: "false"},
		{name: "disabled by file", fileValue: "false"},
		{name: "environment overrides file", envValue: "true", fileValue: "false", want: true},
		{name: "invalid value", envValue: "invalid", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(scannerEnabledEnv, test.envValue)
			if test.fileValue != "" {
				if err := os.WriteFile(os.Getenv(configFileEnv), []byte("SCANNER_ENABLED="+test.fileValue+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load()
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), scannerEnabledEnv) {
					t.Fatalf("expected SCANNER_ENABLED validation error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Scanner().Enabled(); got != test.want {
				t.Errorf("scanner enabled = %t, want %t", got, test.want)
			}
		})
	}
}

func TestLoadRejectsExampleSecret(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(cloudJWTSecretEnv, exampleJWTSecret)

	if _, err := Load(); err == nil {
		t.Fatal("expected the example secret to be rejected")
	}
}

func TestLoadOptionalBetaKey(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   string
		valid bool
	}{
		{name: "empty disables beta key", valid: true},
		{name: "non-empty key", key: "0123456789abcdef", valid: true},
		{name: "short key", key: "short"},
		{name: "example key", key: exampleBetaTestKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(betaTestKeyEnv, test.key)
			cfg, err := Load()
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				if cfg.BetaTestKey() != test.key {
					t.Errorf("unexpected beta key: %q", cfg.BetaTestKey())
				}
			} else if err == nil || !strings.Contains(err.Error(), betaTestKeyEnv) {
				t.Fatalf("expected BETA_TEST_KEY validation error, got %v", err)
			}
		})
	}
}

func TestLoadRequiresUsersDatabaseURL(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(usersDatabaseURLEnv, "")

	if _, err := Load(); err == nil {
		t.Fatal("expected users database URL to be required")
	}
}

func TestLoadRejectsPlanAboveHardLimit(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(userPlansJSONEnv, "[{\"name\":\"user\",\"max_upload_mb\":5,\"max_storage_mb\":100},{\"name\":\"premium\",\"max_upload_mb\":21,\"max_storage_mb\":500}]")

	if _, err := Load(); err == nil {
		t.Fatal("expected a plan limit validation error")
	}
}

func TestLoadRejectsDuplicatePlan(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(userPlansJSONEnv, "[{\"name\":\"user\",\"max_upload_mb\":5,\"max_storage_mb\":100},{\"name\":\"user\",\"max_upload_mb\":10,\"max_storage_mb\":500}]")

	if _, err := Load(); err == nil {
		t.Fatal("expected duplicate plans to be rejected")
	}
}

func TestLoadRejectsUnknownPlanProperty(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(userPlansJSONEnv, "[{\"name\":\"user\",\"max_upload_mb\":5,\"max_storage_mb\":100,\"priority\":1}]")

	if _, err := Load(); err == nil {
		t.Fatal("expected an unknown plan property to be rejected")
	}
}

func TestLoadRejectsSharedQuarantineBucket(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(s3QuarantineBucketEnv, "test-bucket")

	if _, err := Load(); err == nil {
		t.Fatal("expected quarantine bucket to differ from the files bucket")
	}
}

func TestLoadRejectsInsecureConfigurationFilePermissions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "insecure.env")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configPath, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(configFileEnv, configPath)

	if _, err := Load(); err == nil {
		t.Fatal("expected insecure configuration file permissions to be rejected")
	}
}

func setValidEnvironment(t *testing.T) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "empty.env")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	values := map[string]string{
		configFileEnv:         configPath,
		scannerEnabledEnv:     "",
		databaseURLEnv:        "postgres://localhost/test",
		usersDatabaseURLEnv:   "postgres://localhost/lyrarma-services-users",
		betaTestKeyEnv:        "0123456789abcdef",
		cloudJWTSecretEnv:     "0123456789abcdef0123456789abcdef",
		s3BucketEnv:           "test-bucket",
		s3QuarantineBucketEnv: "test-quarantine-bucket",
		s3RegionEnv:           "test-region",
		hardMaxUploadMBEnv:    "20",
		hardMaxStorageMBEnv:   "1000",
		defaultUserPlanEnv:    "user",
		userPlansJSONEnv:      "[{\"name\":\"user\",\"max_upload_mb\":5,\"max_storage_mb\":100},{\"name\":\"premium\",\"max_upload_mb\":10,\"max_storage_mb\":500},{\"name\":\"enterprise-v2\",\"max_upload_mb\":20,\"max_storage_mb\":1000}]",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
