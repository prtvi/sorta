package exifmeta_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prtvi/sorta/internal/exifmeta"
)

func TestReadMissingFile(t *testing.T) {
	_, err := exifmeta.Read(filepath.Join(t.TempDir(), "nope.jpg"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadNoExifReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.jpg")
	// Minimal invalid/no-exif jpeg-like bytes — Decode fails, empty metadata OK.
	if err := os.WriteFile(path, []byte("not-a-jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := exifmeta.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Empty() {
		t.Fatalf("expected empty metadata, got %+v", m)
	}
}

func TestMetadataEmpty(t *testing.T) {
	if !(exifmeta.Metadata{}).Empty() {
		t.Fatal("zero value should be empty")
	}
	if (exifmeta.Metadata{Camera: "Sony"}).Empty() {
		t.Fatal("camera set should not be empty")
	}
}
