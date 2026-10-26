package webui

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
)

func TestRegistrationOptionalBetaKey(t *testing.T) {
	const requiredKey = "0123456789abcdef"
	for _, test := range []struct {
		name             string
		configuredKey    string
		submittedKey     string
		passwordMismatch bool
		apiStatus        int
		wantStatus       int
		wantAPICalls     int
	}{
		{name: "disabled without key", apiStatus: 201, wantStatus: 302, wantAPICalls: 1},
		{name: "disabled ignores supplied key", submittedKey: "unused", apiStatus: 201, wantStatus: 302, wantAPICalls: 1},
		{name: "disabled password mismatch", passwordMismatch: true, wantStatus: 400},
		{name: "disabled API error", apiStatus: 409, wantStatus: 409, wantAPICalls: 1},
		{name: "enabled valid key", configuredKey: requiredKey, submittedKey: requiredKey, apiStatus: 201, wantStatus: 302, wantAPICalls: 1},
		{name: "enabled missing key", configuredKey: requiredKey, wantStatus: 403},
		{name: "enabled invalid key", configuredKey: requiredKey, submittedKey: "incorrect", wantStatus: 403},
		{name: "enabled password mismatch", configuredKey: requiredKey, passwordMismatch: true, wantStatus: 400},
		{name: "enabled API error", configuredKey: requiredKey, submittedKey: requiredKey, apiStatus: 409, wantStatus: 409, wantAPICalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			apiCalls := 0
			app.Post("/api/auth/register", func(c fiber.Ctx) error {
				apiCalls++
				var request models.RegisterRequest
				if err := json.Unmarshal(c.Body(), &request); err != nil {
					t.Errorf("invalid registration payload: %v", err)
				}
				if request.Username != "alice" || request.BetaKey != test.submittedKey {
					t.Errorf("unexpected registration payload: %+v", request)
				}
				if !request.PrivacyPolicyAccepted || !request.IDPolicyAccepted {
					t.Error("registration payload is missing policy acceptance")
				}
				return c.Status(test.apiStatus).JSON(fiber.Map{"message": "registration response"})
			})
			_, err := New(app, Config{
				TemplatesDir:     "../../web/templates",
				TranslationsFile: "../../web/static/translations.json",
				AuthCookieName:   "auth-session",
				BetaTestKey:      test.configuredKey,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			page, err := app.Test(httptest.NewRequest("GET", "/register", nil))
			if err != nil {
				t.Fatal(err)
			}
			pageBody, err := io.ReadAll(page.Body)
			page.Body.Close()
			if err != nil || page.StatusCode != 200 {
				t.Fatalf("registration page failed: status=%d, error=%v", page.StatusCode, err)
			}
			required := test.configuredKey != ""
			if shown := strings.Contains(string(pageBody), `id="beta_key"`); shown != required {
				t.Errorf("beta-key field shown=%t, want %t", shown, required)
			}
			if strings.Contains(string(pageBody), requiredKey) {
				t.Error("configured key leaked into registration HTML")
			}
			form := url.Values{
				"username": {"alice"}, "email": {"alice@example.com"},
				"password": {"test-password"}, "password_confirm": {"test-password"},
				"privacy_policy_accepted": {"true"}, "id_policy_accepted": {"true"},
			}
			if test.submittedKey != "" {
				form.Set("beta_key", test.submittedKey)
			}
			if test.passwordMismatch {
				form.Set("password_confirm", "different-password")
			}
			request := httptest.NewRequest("POST", "/register", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.wantStatus || apiCalls != test.wantAPICalls {
				t.Fatalf("status=%d, API calls=%d; want status=%d, API calls=%d", response.StatusCode, apiCalls, test.wantStatus, test.wantAPICalls)
			}
			if response.StatusCode == 302 {
				if location := response.Header.Get("Location"); location != "/login?registration_pending=1" {
					t.Errorf("unexpected registration redirect: %q", location)
				}
			} else {
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if shown := strings.Contains(string(body), `id="beta_key"`); shown != required {
					t.Errorf("beta-key field on error shown=%t, want %t", shown, required)
				}
			}
		})
	}
}

func TestPendingRegistrationNotice(t *testing.T) {
	app := fiber.New()
	_, err := New(app, Config{
		TemplatesDir: "../../web/templates", TranslationsFile: "../../web/static/translations.json",
		AuthCookieName: "auth-session",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for language, notice := range map[string]string{
		"ru": "Заявка на регистрацию создана", "en": "Your registration request has been created",
	} {
		t.Run(language, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/login?registration_pending=1", nil)
			request.Header.Set("Cookie", "lyrarma_lang="+language)
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || !strings.Contains(string(body), notice) {
				t.Error("missing localized pending registration notice")
			}
			for _, falseNotice := range []string{"Аккаунт создан!", "Account created!"} {
				if strings.Contains(string(body), falseNotice) {
					t.Error("pending registration must not be described as an active account")
				}
			}
		})
	}
}

func TestRegistrationEmailLanguageForwarding(t *testing.T) {
	for _, language := range []string{"ru", "en"} {
		t.Run(language, func(t *testing.T) {
			app := fiber.New()
			forwarded := ""
			app.Post("/api/auth/register", func(c fiber.Ctx) error {
				forwarded = c.Get(fiber.HeaderAcceptLanguage)
				return c.SendStatus(fiber.StatusCreated)
			})
			_, err := New(app, Config{
				TemplatesDir: "../../web/templates", TranslationsFile: "../../web/static/translations.json",
				AuthCookieName: "auth-session",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{
				"username": {"alice"}, "email": {"alice@example.test"},
				"password": {"test-password"}, "password_confirm": {"test-password"},
				"privacy_policy_accepted": {"true"}, "id_policy_accepted": {"true"},
			}
			request := httptest.NewRequest("POST", "/register", strings.NewReader(form.Encode()))
			request.Header.Set(fiber.HeaderContentType, "application/x-www-form-urlencoded")
			request.Header.Set(fiber.HeaderCookie, "lyrarma_lang="+language)
			request.Header.Set(fiber.HeaderAcceptLanguage, "fr-FR")
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 302 || forwarded != language {
				t.Fatalf("registration must forward the selected interface language: status=%d language=%q", response.StatusCode, forwarded)
			}
		})
	}
}
