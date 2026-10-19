package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestPublicPolicies(t *testing.T) {
	app := fiber.New()
	_, err := New(app, Config{
		TemplatesDir:     "../../web/templates",
		TranslationsFile: "../../web/static/translations.json",
		AuthCookieName:   "auth-session",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path    string
		lang    string
		title   string
		section string
	}{
		{path: "/privacy-policy", lang: "en", title: "Lyrarma Cloud Privacy Policy", section: "Data processed and purposes"},
		{path: "/privacy-policy", lang: "ru", title: "Политика конфиденциальности Lyrarma Cloud", section: "Какие данные обрабатываются и зачем"},
		{path: "/lyrarma-id/policy", lang: "en", title: "Lyrarma ID System Policy", section: "Sign-in and account security"},
		{path: "/lyrarma-id/policy", lang: "ru", title: "Политика Lyrarma ID System", section: "Вход и безопасность аккаунта"},
	} {
		t.Run(test.path+"/"+test.lang, func(t *testing.T) {
			request := httptest.NewRequest("GET", test.path, nil)
			request.AddCookie(&http.Cookie{Name: languageCookieName, Value: test.lang})
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") {
				t.Fatalf("unexpected policy response: status=%d, type=%q", response.StatusCode, response.Header.Get("Content-Type"))
			}
			for _, expected := range []string{test.title, test.section, `lang="` + test.lang + `"`, "support@lyrarma.com", "Щекатуров Святослав Кириллович", `href="/register"`, "/set-language/ru?next=", "/set-language/en?next="} {
				if !strings.Contains(string(body), expected) {
					t.Errorf("policy HTML is missing %q", expected)
				}
			}
		})
	}
}

func TestRegistrationPolicyCheckboxes(t *testing.T) {
	app := fiber.New()
	_, err := New(app, Config{
		TemplatesDir:     "../../web/templates",
		TranslationsFile: "../../web/static/translations.json",
		AuthCookieName:   "auth-session",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "ru"} {
		request := httptest.NewRequest("GET", "/register", nil)
		request.AddCookie(&http.Cookie{Name: languageCookieName, Value: lang})
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("registration page: status=%d, error=%v", response.StatusCode, err)
		}
		for _, name := range []string{"privacy_policy_accepted", "id_policy_accepted"} {
			input := regexp.MustCompile(`<input\b[^>]*\bid="` + name + `"[^>]*>`).FindString(string(body))
			for _, attribute := range []string{`type="checkbox"`, `name="` + name + `"`, `value="true"`, "required"} {
				if !strings.Contains(input, attribute) {
					t.Errorf("%s checkbox %s is missing %q", lang, name, attribute)
				}
			}
			if regexp.MustCompile(`\bchecked\b`).MatchString(input) {
				t.Errorf("%s checkbox %s must not be preselected", lang, name)
			}
		}
		for _, path := range []string{"/privacy-policy", "/lyrarma-id/policy"} {
			if !strings.Contains(string(body), `href="`+path+`" target="_blank" rel="noopener noreferrer"`) {
				t.Errorf("%s is missing a policy link to %s", lang, path)
			}
		}
	}
}

func TestRegistrationRejectsUnacceptedPolicies(t *testing.T) {
	for _, test := range []struct {
		name    string
		privacy string
		id      string
	}{
		{name: "neither"},
		{name: "missing privacy", id: "true"},
		{name: "missing ID", privacy: "true"},
		{name: "privacy false", privacy: "false", id: "true"},
		{name: "ID false", privacy: "true", id: "false"},
		{name: "invalid value", privacy: "on", id: "true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			apiCalls := 0
			app.Post("/api/auth/register", func(c fiber.Ctx) error {
				apiCalls++
				return c.SendStatus(201)
			})
			_, err := New(app, Config{
				TemplatesDir:     "../../web/templates",
				TranslationsFile: "../../web/static/translations.json",
				AuthCookieName:   "auth-session",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{
				"username": {"alice"}, "email": {"alice@example.com"},
				"password": {"test-password"}, "password_confirm": {"test-password"},
				"privacy_policy_accepted": {test.privacy}, "id_policy_accepted": {test.id},
			}
			request := httptest.NewRequest("POST", "/register", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.AddCookie(&http.Cookie{Name: languageCookieName, Value: "ru"})
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 400 || apiCalls != 0 || !strings.Contains(string(body), "Для регистрации необходимо отдельно принять обе политики") {
				t.Fatalf("registration not rejected correctly: status=%d, API calls=%d, body=%s", response.StatusCode, apiCalls, body)
			}
		})
	}
}
