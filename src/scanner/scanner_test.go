package scanner

import (
	"math"
	"testing"
)

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
