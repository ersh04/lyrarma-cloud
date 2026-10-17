package webui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/flosch/pongo2/v6"
	"github.com/lyrarma/cloud-api/src/models"
	"golang.org/x/net/html"
)

func TestDashboardFileActions(t *testing.T) {
	loader, err := pongo2.NewLocalFileSystemLoader("../../web/templates")
	if err != nil {
		t.Fatal(err)
	}
	template, err := pongo2.NewSet("dashboard-test", loader).FromFile("dashboard.html")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../web/static/translations.json")
	if err != nil {
		t.Fatal(err)
	}
	var translations map[string]map[string]string
	if err := json.Unmarshal(data, &translations); err != nil {
		t.Fatal(err)
	}

	for _, language := range []string{"ru", "en"} {
		for _, status := range []models.FileStatus{
			models.FileUploading, models.FileScanning, models.FileApproved, models.FileBlocked, models.FileFailed,
		} {
			t.Run(language+"/"+string(status), func(t *testing.T) {
				body, err := template.Execute(pongo2.Context{
					"lang": language,
					"t": func(key string) string {
						return translations[language][key]
					},
					"filesize":          formatFileSize,
					"filetype_icon":     fileTypeIcon,
					"current_folder_id": "root",
					"files": []map[string]any{fileContext(models.FileEntry{
						ID: "test-file", OriginalName: "image.png", ContentType: "image/png", Status: status,
					})},
				})
				if err != nil {
					t.Fatal(err)
				}
				document, err := html.Parse(strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				row := findElementByClass(document, "file-table__row")
				if row == nil {
					t.Fatal("file row not rendered")
				}
				tooltip := findElementByClass(document, "file-status-tooltip")
				if tooltip == nil {
					t.Fatal("custom tooltip not rendered")
				}
				if role, _ := elementAttribute(tooltip, "role"); role != "tooltip" {
					t.Errorf("incorrect tooltip role: %q", role)
				}
				if _, hidden := elementAttribute(tooltip, "hidden"); !hidden {
					t.Error("tooltip should start hidden")
				}
				if tooltip.FirstChild == nil || tooltip.FirstChild.Data != translations[language]["file_scan_pending"] {
					t.Error("incorrect localized tooltip text")
				}
				approved := status == models.FileApproved
				if description, present := elementAttribute(row, "aria-describedby"); present == approved || (!approved && description != "fileStatusTooltip") {
					t.Errorf("incorrect tooltip association: %q for status %s", description, status)
				}
				if focus, present := elementAttribute(row, "tabindex"); present == approved || (!approved && focus != "0") {
					t.Errorf("incorrect row keyboard focus: %q for status %s", focus, status)
				}
				for _, class := range []string{"permission-switch__input", "file-table__share-btn"} {
					node := findElementByClass(row, class)
					if node == nil {
						t.Fatalf("missing control %s", class)
					}
					if _, disabled := elementAttribute(node, "disabled"); disabled == approved {
						t.Errorf("%s disabled=%t for status %s", class, disabled, status)
					}
				}
				download := findElementByClass(row, "file-table__download-btn")
				if download == nil {
					t.Fatal("download control not rendered")
				}
				if href, linked := elementAttribute(download, "href"); linked != approved || (approved && href != "/download/test-file") {
					t.Errorf("download href=%q for status %s", href, status)
				}
				deleteButton := findElementByClass(row, "file-table__delete-btn")
				if deleteButton == nil {
					t.Fatal("delete control not rendered")
				}
				if _, disabled := elementAttribute(deleteButton, "disabled"); disabled {
					t.Error("deletion must stay enabled")
				}
				if !approved {
					for _, node := range []*html.Node{row, download, findElementByClass(row, "permission-switch"), findElementByClass(row, "file-table__share-btn")} {
						if title, present := elementAttribute(node, "title"); present {
							t.Errorf("native title conflicts with custom tooltip: %q", title)
						}
					}
				}
			})
		}
	}
}

func elementAttribute(node *html.Node, name string) (string, bool) {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val, true
		}
	}
	return "", false
}

func findElementByClass(node *html.Node, class string) *html.Node {
	if classes, _ := elementAttribute(node, "class"); strings.Contains(" "+classes+" ", " "+class+" ") {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElementByClass(child, class); found != nil {
			return found
		}
	}
	return nil
}
