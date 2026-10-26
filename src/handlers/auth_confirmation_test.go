package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/middleware"
	"github.com/lyrarma/cloud-api/src/models"
	"github.com/lyrarma/cloud-api/src/storage"
)

type confirmationUsers struct {
	*pendingRegistrationStore
	user  models.User
	err   error
	calls int
	key   string
}

func (s *confirmationUsers) ConfirmRegistration(key string) (models.User, error) {
	s.calls++
	s.key = key
	if s.calls > 1 {
		return models.User{}, storage.ErrRegistrationNotFound
	}
	return s.user, s.err
}

type confirmationProfiles struct {
	calls   int
	err     error
	profile models.UserProfile
}

func (s *confirmationProfiles) EnsureUserProfile(profile models.UserProfile) (models.UserProfile, error) {
	s.calls++
	s.profile = profile
	return profile, s.err
}

func confirmationJWTConfig(t *testing.T) config.JWTConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"CONFIG_FILE": path, "DATABASE_URL": "postgres://localhost/test", "USERS_DATABASE_URL": "postgres://localhost/users",
		"JWT_SECRET": "test-signing-secret-with-at-least-32-bytes", "JWT_TTL": "24h", "BETA_TEST_KEY": "",
		"S3_BUCKET": "test", "S3_QUARANTINE_BUCKET": "quarantine", "AWS_REGION": "test", "AWS_ACCESS_KEY_ID": "", "AWS_SECRET_ACCESS_KEY": "",
		"MAX_UPLOAD_MB": "10", "MAX_STORAGE_MB": "100", "DEFAULT_USER_PLAN": "user",
		"USER_PLANS_JSON": `[{"name":"user","max_upload_mb":10,"max_storage_mb":100}]`,
	} {
		t.Setenv(key, value)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.JWT()
}

func TestConfirmRegistration(t *testing.T) {
	jwtConfig := confirmationJWTConfig(t)
	for _, test := range []struct {
		name       string
		storeErr   error
		profileErr error
		status     int
		code       string
	}{
		{name: "success", status: 200},
		{name: "unknown or replayed key", storeErr: storage.ErrRegistrationNotFound, status: 404, code: "confirmation_invalid"},
		{name: "expired", storeErr: storage.ErrRegistrationExpired, status: 404, code: "confirmation_expired"},
		{name: "username conflict", storeErr: storage.ErrUserAlreadyExists, status: 409, code: "confirmation_conflict"},
		{name: "email conflict", storeErr: storage.ErrEmailAlreadyUsed, status: 409, code: "confirmation_conflict"},
		{name: "database failure", storeErr: errors.New("database unavailable"), status: 500, code: "internal_error"},
		{name: "cloud unavailable after confirmation", profileErr: errors.New("cloud unavailable"), status: 503, code: "confirmation_session_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const key = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
			users := &confirmationUsers{user: models.User{ID: "account-id", Username: "alice", Email: "alice@example.test", Password: "private-password-hash", Plan: []string{"user"}}, err: test.storeErr}
			profiles := &confirmationProfiles{err: test.profileErr}
			var logs bytes.Buffer
			h := &AuthHandler{userStore: users, cloudStore: profiles, jwt: jwtConfig, logger: slog.New(slog.NewTextHandler(&logs, nil))}
			app := fiber.New()
			app.Get("/confirm", h.ConfirmRegistration)
			response, err := app.Test(httptest.NewRequest("GET", "/confirm?key="+key, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.status || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("unexpected confirmation response: status=%d", response.StatusCode)
			}
			if users.key != key || users.calls != 1 {
				t.Fatal("incorrect confirmation lookup")
			}
			if test.storeErr != nil && profiles.calls != 0 {
				t.Fatal("failed confirmation must not initialize a profile")
			}
			for _, secret := range []string{key, "private-password-hash", "alice@example.test"} {
				if strings.Contains(string(body), secret) || strings.Contains(logs.String(), secret) {
					t.Fatal("confirmation leaked private registration data")
				}
			}
			if test.status != 200 {
				var result models.ErrorResponse
				if err := json.Unmarshal(body, &result); err != nil || result.Error != test.code {
					t.Fatalf("unexpected confirmation error: %s", body)
				}
				return
			}
			var result models.RegistrationConfirmationResponse
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			claims := &middleware.Claims{}
			token, err := jwt.ParseWithClaims(result.Token, claims, func(*jwt.Token) (any, error) { return []byte(jwtConfig.Secret()), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("lyrarma-services-users"))
			if err != nil || !token.Valid || claims.UserID != "account-id" || result.Username != "alice" || !result.ExpiresAt.After(time.Now()) {
				t.Fatal("confirmation must issue a valid session for the confirmed account")
			}
			if profiles.profile.ID != "account-id" || profiles.calls != 1 {
				t.Fatal("incorrect confirmed cloud profile")
			}
			replayed, err := app.Test(httptest.NewRequest("GET", "/confirm?key="+key, nil))
			if err != nil {
				t.Fatal(err)
			}
			replayed.Body.Close()
			if replayed.StatusCode != 404 || profiles.calls != 1 {
				t.Fatal("replayed keys must not issue another session")
			}
		})
	}
}
