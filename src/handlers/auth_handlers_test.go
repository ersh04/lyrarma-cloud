package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
)

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
			payload, err := json.Marshal(models.RegisterRequest{BetaKey: test.supplied})
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
