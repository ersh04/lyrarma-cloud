package webui

import (
	"io"
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/lyrarma/cloud-api/src/models"
	"golang.org/x/net/html"
)

func TestStorageContext(t *testing.T) {
	const gb = int64(1024 * 1024 * 1024)
	for _, test := range []struct {
		name      string
		used      int64
		reserved  int64
		limit     int64
		percent   string
		state     string
		available bool
	}{
		{name: "empty", limit: 10 * gb, percent: "0.00", state: "normal", available: true},
		{name: "includes reservations", used: 2 * gb, reserved: gb, limit: 10 * gb, percent: "30.00", state: "normal", available: true},
		{name: "larger plan", used: 2 * gb, reserved: gb, limit: 100 * gb, percent: "3.00", state: "normal", available: true},
		{name: "almost full", used: 8 * gb, limit: 10 * gb, percent: "80.00", state: "warning", available: true},
		{name: "full", used: 10 * gb, limit: 10 * gb, percent: "100.00", state: "full", available: true},
		{name: "downgraded plan", used: 12 * gb, limit: 10 * gb, percent: "100.00", state: "full", available: true},
		{name: "no integer overflow", used: math.MaxInt64, reserved: math.MaxInt64, limit: math.MaxInt64, percent: "100.00", state: "full", available: true},
		{name: "invalid limit"},
		{name: "invalid usage", used: -1, limit: gb},
		{name: "invalid reservation", reserved: -1, limit: gb},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := storageContext(&dashboardLimits{
				Plan: []string{"user", "premium"}, MaxStorageSize: test.limit,
				UsedStorageSize: test.used, ReservedStorageSize: test.reserved,
			}, "en")
			if result["available"] != test.available {
				t.Fatalf("unexpected availability: %v", result)
			}
			if test.available && (result["percent"] != test.percent || result["state"] != test.state || result["plan"] != "user, premium") {
				t.Errorf("unexpected quota display: %v", result)
			}
		})
	}
	if storageContext(nil, "en")["available"] != false {
		t.Error("missing quota must not appear as empty storage")
	}
}

func TestFormatStorageGB(t *testing.T) {
	for _, test := range []struct {
		bytes float64
		lang  string
		want  string
	}{
		{bytes: 0, lang: "en", want: "0.00"},
		{bytes: 1, lang: "en", want: "<0.01"},
		{bytes: 1, lang: "ru", want: "<0,01"},
		{bytes: 1024 * 1024 * 1024, lang: "en", want: "1.00"},
		{bytes: 1.5 * 1024 * 1024 * 1024, lang: "ru", want: "1,50"},
	} {
		if got := formatStorageGB(test.bytes, test.lang); got != test.want {
			t.Errorf("formatStorageGB(%v, %q) = %q, want %q", test.bytes, test.lang, got, test.want)
		}
	}
}

func TestDashboardStorageUsage(t *testing.T) {
	const gb = int64(1024 * 1024 * 1024)
	for _, language := range []string{"ru", "en"} {
		t.Run(language, func(t *testing.T) {
			limits := dashboardLimits{
				Plan: []string{"premium"}, MaxUploadSize: gb, MaxStorageSize: 10 * gb,
				UsedStorageSize: 4 * gb, ReservedStorageSize: gb / 4,
			}
			app := fiber.New()
			app.Get("/api/files", func(c fiber.Ctx) error {
				return c.JSON(models.FileListResponse{Files: []models.FileEntry{
					{ID: "small-file", OriginalName: "small.txt", Size: 10, Status: models.FileApproved},
				}})
			})
			app.Get("/api/folders", func(c fiber.Ctx) error {
				return c.JSON(models.FolderListResponse{Folders: []models.FolderEntry{
					{ID: "work", Name: "Work", ParentID: "root"},
				}})
			})
			apiCalls := 0
			app.Get("/api/info/limits", func(c fiber.Ctx) error {
				apiCalls++
				if c.Get("Authorization") != "Bearer session-token" {
					t.Error("limits request did not use the authenticated session")
				}
				return c.JSON(limits)
			})
			_, err := New(app, Config{
				TemplatesDir: "../../web/templates", TranslationsFile: "../../web/static/translations.json",
				AuthCookieName: "auth-session", MaxUploadSize: gb,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("GET", "/dashboard?folder_id=work", nil)
			request.Header.Set("Cookie", "auth-session=session-token; lyrarma_lang="+language)
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || response.StatusCode != 200 {
				t.Fatalf("dashboard failed: status=%d, error=%v", response.StatusCode, err)
			}
			document, err := html.Parse(strings.NewReader(string(body)))
			if err != nil {
				t.Fatal(err)
			}
			meter := findElementByClass(document, "storage-meter")
			if meter == nil {
				t.Fatal("storage meter not rendered")
			}
			if state, _ := elementAttribute(meter, "data-state"); state != "normal" {
				t.Errorf("unexpected meter state: %q", state)
			}
			progress := findElementByClass(meter, "storage-meter__track")
			if progress == nil {
				t.Fatal("storage progress bar not rendered")
			}
			if value, _ := elementAttribute(progress, "aria-valuenow"); value != "42.50" {
				t.Errorf("usage must include global files and reservations, got %q", value)
			}
			wantText := "4.25 GB of 10.00 GB"
			if language == "ru" {
				wantText = "4,25 ГБ из 10,00 ГБ"
			}
			if text, _ := elementAttribute(progress, "aria-valuetext"); text != wantText {
				t.Errorf("unexpected localized amounts: %q, want %q", text, wantText)
			}
			for _, authenticated := range []bool{false, true} {
				before := apiCalls
				request := httptest.NewRequest("GET", "/storage/usage", nil)
				request.Header.Set("Authorization", "Bearer forged-token")
				if authenticated {
					request.Header.Set("Cookie", "auth-session=session-token")
				}
				response, err := app.Test(request)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if authenticated {
					if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" || apiCalls != before+1 {
						t.Error("authenticated usage refresh failed")
					}
				} else if response.StatusCode != 302 || apiCalls != before {
					t.Error("anonymous usage request was not rejected")
				}
			}
		})
	}
}
