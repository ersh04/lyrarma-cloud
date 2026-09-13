package config

import (
	"os"
	"path/filepath"
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
}

func TestLoadRejectsExampleSecret(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(cloudJWTSecretEnv, exampleJWTSecret)

	if _, err := Load(); err == nil {
		t.Fatal("expected the example secret to be rejected")
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
		configFileEnv:       configPath,
		databaseURLEnv:      "postgres://localhost/test",
		betaTestKeyEnv:      "0123456789abcdef",
		cloudJWTSecretEnv:   "0123456789abcdef0123456789abcdef",
		s3BucketEnv:         "test-bucket",
		s3RegionEnv:         "test-region",
		hardMaxUploadMBEnv:  "20",
		hardMaxStorageMBEnv: "1000",
		defaultUserPlanEnv:  "user",
		userPlansJSONEnv:    "[{\"name\":\"user\",\"max_upload_mb\":5,\"max_storage_mb\":100},{\"name\":\"premium\",\"max_upload_mb\":10,\"max_storage_mb\":500},{\"name\":\"enterprise-v2\",\"max_upload_mb\":20,\"max_storage_mb\":1000}]",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
