package handlers

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
	"github.com/lyrarma/cloud-api/src/storage"
	"golang.org/x/crypto/bcrypt"
)

type pendingRegistrationStore struct {
	requests []models.PendingRegistration
	err      error
}

func (s *pendingRegistrationStore) CreatePendingRegistration(request models.PendingRegistration) error {
	s.requests = append(s.requests, request)
	return s.err
}

func (s *pendingRegistrationStore) GetUserByUsername(string) (models.User, error) {
	return models.User{}, storage.ErrUserNotFound
}

func (s *pendingRegistrationStore) ConfirmRegistration(string) (models.User, error) {
	return models.User{}, storage.ErrRegistrationNotFound
}

func TestRegisterCreatesOnlyPendingRegistration(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "created", status: 201},
		{name: "username conflict", err: storage.ErrUserAlreadyExists, status: 409, code: "user_exists"},
		{name: "email conflict", err: storage.ErrEmailAlreadyUsed, status: 409, code: "email_exists"},
		{name: "database error", err: errors.New("database unavailable"), status: 500, code: "internal_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &pendingRegistrationStore{err: test.err}
			var logs bytes.Buffer
			handler := &AuthHandler{userStore: store, defaultPlan: "user", logger: slog.New(slog.NewTextHandler(&logs, nil))}
			app := fiber.New(fiber.Config{StreamRequestBody: true})
			app.Post("/register", handler.Register)
			app.Post("/login", handler.Login)
			payload := `{"username":" alice ","email":" Alice@Example.COM ","password":"test-password","privacy_policy_accepted":true,"id_policy_accepted":true}`
			request := httptest.NewRequest("POST", "/register", strings.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			before := time.Now().UTC()
			response, err := app.Test(request, fiber.TestConfig{Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body map[string]any
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.status || (test.code != "" && body["error"] != test.code) {
				t.Fatalf("status=%d body=%v", response.StatusCode, body)
			}
			if len(store.requests) != 1 {
				t.Fatalf("got %d pending registration writes", len(store.requests))
			}
			pending := store.requests[0]
			if pending.Username != "alice" || pending.Email != "alice@example.com" || len(pending.Plan) != 1 || pending.Plan[0] != "user" {
				t.Fatal("pending registration identity or plan is incorrect")
			}
			if pending.CreatedAt.Before(before) || pending.CreatedAt.After(time.Now().UTC()) || pending.ExpiresAt.Sub(pending.CreatedAt) != 24*time.Hour {
				t.Fatal("incorrect pending registration lifetime")
			}
			if err := bcrypt.CompareHashAndPassword([]byte(pending.PasswordHash), []byte("test-password")); err != nil {
				t.Fatal("pending registration does not contain the password hash")
			}
			key, err := hex.DecodeString(pending.ConfirmationKey)
			if err != nil || len(key) != 32 {
				t.Fatal("confirmation key must contain 32 random bytes encoded as hexadecimal")
			}
			encodedBody, _ := json.Marshal(body)
			encodedPending, _ := json.Marshal(pending)
			for _, secret := range []string{pending.ConfirmationKey, pending.PasswordHash, "test-password"} {
				if strings.Contains(string(encodedBody), secret) || strings.Contains(logs.String(), secret) || strings.Contains(string(encodedPending), secret) {
					t.Fatal("registration secret exposed in a response, log or JSON encoding")
				}
			}
			if test.err == nil {
				if body["message"] != "registration pending confirmation" {
					t.Fatalf("unexpected registration response: %v", body)
				}
				login := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"alice","password":"test-password"}`))
				login.Header.Set("Content-Type", "application/json")
				loginResponse, err := app.Test(login)
				if err != nil {
					t.Fatal(err)
				}
				loginResponse.Body.Close()
				if loginResponse.StatusCode != 401 {
					t.Fatal("a pending registration must not be able to sign in")
				}
			}
		})
	}
}

func TestGenerateRegistrationKey(t *testing.T) {
	seen := make(map[string]bool)
	for range 32 {
		key, err := generateRegistrationKey()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 || seen[key] {
			t.Fatal("confirmation keys must be unique, URL-safe 256-bit values")
		}
		seen[key] = true
	}
}

func TestConfirmRegistrationMissingKey(t *testing.T) {
	handler := &AuthHandler{userStore: &pendingRegistrationStore{}}
	app := fiber.New()
	app.Get("/confirm-registration", handler.ConfirmRegistration)
	response, err := app.Test(httptest.NewRequest("GET", "/confirm-registration", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body models.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 404 || body.Error != "confirmation_invalid" {
		t.Fatalf("unexpected confirmation result: status=%d body=%v", response.StatusCode, body)
	}
}

func TestRegisterOptionalBetaKey(t *testing.T) {
	const key = "0123456789abcdef"
	for _, test := range []struct {
		name       string
		configured string
		supplied   string
		wantStatus int
		wantError  string
	}{
		{name: "disabled without key", wantStatus: 400, wantError: "validation_error"},
		{name: "disabled ignores key", supplied: "unused", wantStatus: 400, wantError: "validation_error"},
		{name: "enabled missing key", configured: key, wantStatus: 403, wantError: "invalid_beta_key"},
		{name: "enabled incorrect key", configured: key, supplied: "wrong", wantStatus: 403, wantError: "invalid_beta_key"},
		{name: "enabled matching key", configured: key, supplied: key, wantStatus: 400, wantError: "validation_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &AuthHandler{registrationKey: test.configured}
			app := fiber.New(fiber.Config{StreamRequestBody: true})
			app.Post("/register", func(c fiber.Ctx) error {
				handler.Register(c)
				return nil
			})
			payload, err := json.Marshal(models.RegisterRequest{BetaKey: test.supplied, PrivacyPolicyAccepted: true, IDPolicyAccepted: true})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "/register", strings.NewReader(string(payload)))
			request.Header.Set("Content-Type", "application/json")
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var result models.ErrorResponse
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.wantStatus || result.Error != test.wantError {
				t.Errorf("status=%d, error=%q; want status=%d, error=%q", response.StatusCode, result.Error, test.wantStatus, test.wantError)
			}
		})
	}
}

func TestRegisterRequiresBothPolicies(t *testing.T) {
	for _, test := range []struct {
		name      string
		payload   string
		wantError string
	}{
		{name: "missing both", payload: `{}`, wantError: "policies_not_accepted"},
		{name: "missing privacy", payload: `{"id_policy_accepted":true}`, wantError: "policies_not_accepted"},
		{name: "missing ID", payload: `{"privacy_policy_accepted":true}`, wantError: "policies_not_accepted"},
		{name: "privacy false", payload: `{"privacy_policy_accepted":false,"id_policy_accepted":true}`, wantError: "policies_not_accepted"},
		{name: "ID false", payload: `{"privacy_policy_accepted":true,"id_policy_accepted":false}`, wantError: "policies_not_accepted"},
		{name: "null", payload: `{"privacy_policy_accepted":null,"id_policy_accepted":null}`, wantError: "policies_not_accepted"},
		{name: "privacy string", payload: `{"privacy_policy_accepted":"true","id_policy_accepted":true}`, wantError: "invalid_body"},
		{name: "ID string", payload: `{"privacy_policy_accepted":true,"id_policy_accepted":"true"}`, wantError: "invalid_body"},
		{name: "both accepted", payload: `{"privacy_policy_accepted":true,"id_policy_accepted":true}`, wantError: "validation_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &AuthHandler{}
			app := fiber.New(fiber.Config{StreamRequestBody: true})
			app.Post("/register", func(c fiber.Ctx) error {
				handler.Register(c)
				return nil
			})
			request := httptest.NewRequest("POST", "/register", strings.NewReader(test.payload))
			request.Header.Set("Content-Type", "application/json")
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var result models.ErrorResponse
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 400 || result.Error != test.wantError {
				t.Errorf("status=%d, error=%q; want status=400, error=%q", response.StatusCode, result.Error, test.wantError)
			}
		})
	}
}

func TestValidEmail(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{value: "alice@example.com", valid: true},
		{value: "alice+cloud@example.com", valid: true},
		{value: "", valid: false},
		{value: "alice", valid: false},
		{value: "Alice <alice@example.com>", valid: false},
	}

	for _, test := range tests {
		if got := validEmail(test.value); got != test.valid {
			t.Errorf("validEmail(%q) = %t, want %t", test.value, got, test.valid)
		}
	}
}
