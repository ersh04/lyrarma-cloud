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
				fileList := findElementByClass(document, "file-list")
				if fileList == nil {
					t.Fatal("scrollable file list not rendered")
				}
				if role, _ := elementAttribute(fileList, "role"); role != "region" {
					t.Errorf("incorrect file list role: %q", role)
				}
				if focus, _ := elementAttribute(fileList, "tabindex"); focus != "0" {
					t.Error("file list must support keyboard scrolling")
				}
				table := findElementByClass(fileList, "file-table")
				if table == nil {
					t.Fatal("file table not rendered")
				}
				if role, _ := elementAttribute(table, "role"); role != "table" {
					t.Error("responsive file list must retain its accessible table role")
				}
				title := findElementByClass(document, "workspace-head__title")
				if title == nil {
					t.Fatal("file list title not rendered")
				}
				titleID, _ := elementAttribute(title, "id")
				if label, _ := elementAttribute(fileList, "aria-labelledby"); titleID == "" || label != titleID {
					t.Error("file list must reference its localized title")
				}
				if title.FirstChild == nil || title.FirstChild.Data != translations[language]["file_manager"] {
					t.Error("incorrect localized file list title")
				}
				for _, class := range []string{"upload-card", "file-manager__sidebar", "workspace-head", "storage-meter"} {
					if findElementByClass(fileList, class) != nil {
						t.Errorf("%s must remain outside the scrollable file list", class)
					}
				}
				row := findElementByClass(document, "file-table__row")
				if row == nil {
					t.Fatal("file row not rendered")
				}
				if findElementByClass(fileList, "file-table__row") != row {
					t.Error("file rows must be inside the scrollable file list")
				}
				if role, _ := elementAttribute(row, "role"); role != "row" {
					t.Error("file cards must retain their accessible table row role")
				}
				for class, key := range map[string]string{
					"file-table__type": "file_type", "file-table__size": "file_size",
					"file-table__date": "upload_date", "file-table__access": "access",
				} {
					cell := findElementByClass(row, class)
					if cell == nil {
						t.Fatalf("missing file card field %s", class)
					}
					if label, _ := elementAttribute(cell, "data-label"); label == "" || label != translations[language][key] {
						t.Errorf("incorrect localized mobile label for %s: %q", class, label)
					}
					if role, _ := elementAttribute(cell, "role"); role != "cell" {
						t.Errorf("file card field %s must retain its accessible table cell role", class)
					}
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
