package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
)

func TestRegistrationEmailPayload(t *testing.T) {
	for _, test := range []struct {
		language string
		subject  string
		greeting string
	}{
		{language: "", subject: "Lyrarma ID Registration Confirmation", greeting: "Hello!"},
		{language: "en-US", subject: "Lyrarma ID Registration Confirmation", greeting: "Hello!"},
		{language: "ru", subject: "Подтверждение регистрации Lyrarma ID", greeting: "Здравствуйте!"},
		{language: "ru-RU,en;q=0.8", subject: "Подтверждение регистрации Lyrarma ID", greeting: "Здравствуйте!"},
		{language: "ru;q=0.1,en;q=0.9", subject: "Lyrarma ID Registration Confirmation", greeting: "Hello!"},
		{language: "fr", subject: "Lyrarma ID Registration Confirmation", greeting: "Hello!"},
	} {
		t.Run(test.language, func(t *testing.T) {
			const key = "key+with&reserved=?/%characters"
			payload, err := registrationEmailPayload("alice@example.test", key, "https://cloud.example.test/confirm?source=email&key=old&key=older", test.language)
			if err != nil {
				t.Fatal(err)
			}
			if payload.Email != "alice@example.test" || payload.Subject != test.subject || !strings.HasPrefix(payload.Text, test.greeting+"\n") {
				t.Fatal("incorrect localized registration email")
			}
			_, rawURL, found := strings.Cut(payload.Text, "https://")
			if !found || strings.Contains(payload.Text, "{confirmation_url}") {
				t.Fatal("confirmation URL placeholder was not replaced")
			}
			link, err := url.Parse("https://" + rawURL)
			if err != nil {
				t.Fatal(err)
			}
			if link.Host != "cloud.example.test" || link.Path != "/confirm" || link.Query().Get("source") != "email" || link.Query().Get("key") != key || len(link.Query()["key"]) != 1 {
				t.Fatal("confirmation URL must use the configured address and safely encode exactly one key")
			}
		})
	}
}

func TestRegistrationEmailRejectsInvalidURL(t *testing.T) {
	for _, rawURL := range []string{"", "/confirm", "javascript:confirm", "https:///confirm", "https://user:password@example.test", "https://example.test/#fragment", "https://%zz"} {
		if _, err := registrationEmailPayload("alice@example.test", "key", rawURL, "en"); err == nil {
			t.Error("invalid confirmation URL was accepted")
		}
	}
	if _, err := registrationEmailPayload("alice@example.test", "", "https://example.test/confirm", "en"); err == nil {
		t.Error("empty confirmation key was accepted")
	}
}

func TestSendRegistrationKeyUsesLocalizedJSON(t *testing.T) {
	for _, language := range []string{"ru", "en"} {
		t.Run(language, func(t *testing.T) {
			payloads := make(chan models.ConfirmationRegistrationRequest, 1)
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/send-email" || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("incorrect post controller request")
				}
				var payload models.ConfirmationRegistrationRequest
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error("invalid email JSON")
				}
				payloads <- payload
				w.WriteHeader(http.StatusOK)
			}))
			defer remote.Close()
			handler := &AuthHandler{
				postControllerURL: remote.URL, postControllerToken: "test-token",
				registrationConfirmationURL: "https://cloud.example.test/confirm",
			}
			app := fiber.New()
			app.Post("/send", func(c fiber.Ctx) error {
				handler.SendRegistrationKey(c, "alice@example.test", "private-key")
				return nil
			})
			request := httptest.NewRequest("POST", "/send", nil)
			request.Header.Set("Accept-Language", language)
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("send status = %d", response.StatusCode)
			}
			var payload models.ConfirmationRegistrationRequest
			select {
			case payload = <-payloads:
			default:
				t.Fatal("post controller did not receive an email request")
			}
			expected, err := registrationEmailPayload("alice@example.test", "private-key", handler.registrationConfirmationURL, language)
			if err != nil || payload != expected {
				t.Error("post controller did not receive the localized email and configured link")
			}
		})
	}
}

func TestSendRegistrationKeyTransportFailure(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	remote.Close()
	handler := &AuthHandler{postControllerURL: remote.URL, registrationConfirmationURL: "https://cloud.example.test/confirm"}
	app := fiber.New()
	app.Post("/send", func(c fiber.Ctx) error {
		handler.SendRegistrationKey(c, "alice@example.test", "private-key")
		return nil
	})
	response, err := app.Test(httptest.NewRequest("POST", "/send", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 500 {
		t.Fatal("transport errors must return without accessing a nil response")
	}
}
