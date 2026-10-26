package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
)

func TestConfirmationRedirectAndCookies(t *testing.T) {
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	app := fiber.New()
	app.Get("/api/auth/confirm-registration", func(c fiber.Ctx) error {
		if c.Query("key") != "private-key" {
			t.Error("confirmation key was not forwarded")
		}
		return c.JSON(models.RegistrationConfirmationResponse{LoginResponse: models.LoginResponse{Token: "signed-token", ExpiresAt: expires}, Username: "alice"})
	})
	_, err := New(app, Config{TemplatesDir: "../../web/templates", TranslationsFile: "../../web/static/translations.json", AuthCookieName: "auth-session", CookiePath: "/", CookieHTTPOnly: true, CookieSecure: true, CookieSameSite: "Lax", TokenTTL: 24 * time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := app.Test(httptest.NewRequest("GET", "/confirm-registration?key=private-key", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 302 || response.Header.Get("Location") != "/dashboard" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("confirmation must redirect without leaking the key")
	}
	cookies := response.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("got %d session cookies", len(cookies))
	}
	for _, cookie := range cookies {
		if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 || cookie.MaxAge > 3600 || (!cookie.Expires.IsZero() && !cookie.Expires.Equal(expires)) {
			t.Fatalf("incorrect session cookie settings: HttpOnly=%t Secure=%t Path=%q Expires=%v MaxAge=%d", cookie.HttpOnly, cookie.Secure, cookie.Path, cookie.Expires, cookie.MaxAge)
		}
		switch cookie.Name {
		case "auth-session":
			if cookie.Value != "signed-token" {
				t.Error("incorrect session token")
			}
		case "auth-session-username":
			if cookie.Value != "alice" {
				t.Error("incorrect session username")
			}
		default:
			t.Error("unexpected confirmation cookie")
		}
	}
}

func TestConfirmationErrorsAreLocalized(t *testing.T) {
	for _, lang := range []string{"ru", "en"} {
		for _, test := range []struct {
			code   string
			status int
			ru     string
			en     string
		}{
			{code: "confirmation_invalid", status: 404, ru: "Ссылка подтверждения недействительна", en: "The confirmation link is invalid"},
			{code: "confirmation_expired", status: 404, ru: "Срок действия ссылки истёк", en: "The confirmation link has expired"},
			{code: "confirmation_conflict", status: 409, ru: "Аккаунт с таким именем", en: "An account with this username"},
			{code: "confirmation_session_failed", status: 503, ru: "Аккаунт подтверждён", en: "Your account is confirmed"},
			{code: "malformed_response", status: 200, ru: "Сервер вернул некорректный ответ", en: "The server returned an invalid response"},
		} {
			t.Run(lang+"/"+test.code, func(t *testing.T) {
				app := fiber.New()
				app.Get("/api/auth/confirm-registration", func(c fiber.Ctx) error { return c.Status(test.status).JSON(fiber.Map{"error": test.code}) })
				_, err := New(app, Config{TemplatesDir: "../../web/templates", TranslationsFile: "../../web/static/translations.json", AuthCookieName: "auth-session"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest("GET", "/confirm-registration?key=private-key", nil)
				request.Header.Set("Cookie", "lyrarma_lang="+lang)
				response, err := app.Test(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				status := test.status
				if status == 200 {
					status = 502
				}
				notice := test.en
				if lang == "ru" {
					notice = test.ru
				}
				if response.StatusCode != status || !strings.Contains(string(body), notice) || len(response.Cookies()) != 0 || response.Header.Get("Referrer-Policy") != "no-referrer" {
					t.Fatal("failed confirmation must show a localized error without creating a session")
				}
			})
		}
	}
}
