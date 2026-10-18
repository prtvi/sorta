package scanner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prtvi/sorta/internal/scanner"
)

func TestScanRootFindsSupportedFormats(t *testing.T) {
	dir := t.TempDir()
	files := []string{"a.JPG", "b.jpeg", "c.PNG", "d.webp", "e.HEIC", "f.heif", "g.txt", "h.gif"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Classification dirs and nested files must be ignored.
	for _, sub := range []string{"liked", "shortlisted", "posting", "disliked", "sharing", "posted"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "nested.jpg"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "deep.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	photos, err := scanner.ScanRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(photos) != 6 {
		t.Fatalf("expected 6 photos, got %d: %+v", len(photos), photos)
	}
	// Deterministic alphabetical order.
	want := []string{"a.JPG", "b.jpeg", "c.PNG", "d.webp", "e.HEIC", "f.heif"}
	for i, w := range want {
		if photos[i].Name != w {
			t.Errorf("photos[%d]=%q want %q", i, photos[i].Name, w)
		}
		if photos[i].URL != "/photos/"+w {
			t.Errorf("url=%q", photos[i].URL)
		}
	}
}

func TestBytesInDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.ARW"), []byte("rawraw"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "liked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "liked", "nested.jpg"), []byte("xxxxxxxxxx"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := scanner.BytesInDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(5 + 6)
	if got != want {
		t.Fatalf("BytesInDir=%d want %d", got, want)
	}

	missing, err := scanner.BytesInDir(filepath.Join(dir, "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("missing dir size=%d want 0", missing)
	}
}

func TestBytesInTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "liked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "liked", "nested.jpg"), []byte("xxxxxxxxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".sorta", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sorta", "cache", "x.jpg"), []byte("cached!!!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := scanner.BytesInTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(5 + 10) // root + liked; .sorta skipped
	if got != want {
		t.Fatalf("BytesInTree=%d want %d", got, want)
	}
}

func TestIsSupportedImage(t *testing.T) {
	cases := map[string]bool{
		"x.jpg":  true,
		"x.JPEG": true,
		"x.Png":  true,
		"x.WEBP": true,
		"x.heic": true,
		"x.HEIF": true,
		"x.txt":  false,
		"x":      false,
	}
	for name, want := range cases {
		if got := scanner.IsSupportedImage(name); got != want {
			t.Errorf("IsSupportedImage(%q)=%v want %v", name, got, want)
		}
	}
}
