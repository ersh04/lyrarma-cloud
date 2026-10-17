package scanner

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/models"
)

func TestDisabledScannerSkipsInspection(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	service := New(nil, nil, config.ScannerConfig{}, nil)
	result := service.inspect(context.Background(), models.FileEntry{
		OriginalName: "program.exe",
		ContentType:  "application/x-dosexec",
	})
	if result.status != models.FileApproved || result.reason != "" {
		t.Fatalf("expected approval without inspection, got %+v", result)
	}
	if result.detectedContentType != "" || result.sha256 != "" {
		t.Fatalf("uninspected file must not have detected metadata, got %+v", result)
	}
}

func TestArchiveMetricsCountsOnlyFiles(t *testing.T) {
	listing := []byte("7-Zip 24.00\r\n\r\n----------\r\nPath = folder\r\nSize = 0\r\nFolder = +\r\n\r\nPath = folder/a.txt\r\nSize = 12\r\nFolder = -\r\n\r\nPath = b.bin\r\nSize = 30\r\nFolder = -\r\n")

	files, size, err := archiveMetrics(listing)
	if err != nil {
		t.Fatal(err)
	}
	if files != 2 || size != 42 {
		t.Fatalf("unexpected archive metrics: files=%d size=%d", files, size)
	}
}

func TestArchiveMetricsRejectsInvalidSize(t *testing.T) {
	listing := []byte("----------\nPath = file\nSize = invalid\nFolder = -\n")
	if _, _, err := archiveMetrics(listing); err == nil {
		t.Fatal("expected invalid archive size to be rejected")
	}
}

func TestSafeReadLimit(t *testing.T) {
	if limit := safeReadLimit(10); limit != 11 {
		t.Fatalf("unexpected read limit: %d", limit)
	}
	if limit := safeReadLimit(math.MaxInt64); limit != math.MaxInt64 {
		t.Fatalf("unexpected maximum read limit: %d", limit)
	}
}
