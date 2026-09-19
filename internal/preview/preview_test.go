package preview_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/prtvi/sorta/internal/preview"
)

func TestNeedsTranscode(t *testing.T) {
	if !preview.NeedsTranscode("IMG.HEIC") {
		t.Fatal("expected HEIC to need transcode")
	}
	if !preview.NeedsTranscode("a.heif") {
		t.Fatal("expected HEIF to need transcode")
	}
	if preview.NeedsTranscode("a.jpg") {
		t.Fatal("jpg should not need transcode")
	}
}

func TestParseSize(t *testing.T) {
	if preview.ParseSize("thumb") != preview.SizeThumb {
		t.Fatal("expected thumb")
	}
	if preview.ParseSize("") != preview.SizeView {
		t.Fatal("default view")
	}
}

func TestDisplayJPEGThumbAndView(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sips only on macOS")
	}
	if _, err := exec.LookPath("sips"); err != nil {
		t.Skip("sips not available")
	}

	dir := t.TempDir()
	pngPath := filepath.Join(dir, "src.png")
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(pngPath, png, 0o644); err != nil {
		t.Fatal(err)
	}

	thumb, err := preview.DisplayJPEG(dir, pngPath, preview.SizeThumb)
	if err != nil {
		t.Fatal(err)
	}
	view, err := preview.DisplayJPEG(dir, pngPath, preview.SizeView)
	if err != nil {
		t.Fatal(err)
	}
	if thumb == view {
		t.Fatal("thumb and view cache paths should differ")
	}
	for _, p := range []string{thumb, view} {
		info, err := os.Stat(p)
		if err != nil || info.Size() == 0 {
			t.Fatalf("expected cached jpeg at %s: %v %v", p, info, err)
		}
	}

	thumb2, err := preview.DisplayJPEG(dir, pngPath, preview.SizeThumb)
	if err != nil || thumb2 != thumb {
		t.Fatalf("thumb cache miss: %v %s", err, thumb2)
	}
}

func TestJPEGForWithSips(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sips only on macOS")
	}
	if _, err := exec.LookPath("sips"); err != nil {
		t.Skip("sips not available")
	}

	dir := t.TempDir()
	pngPath := filepath.Join(dir, "src.png")
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(pngPath, png, 0o644); err != nil {
		t.Fatal(err)
	}
	heicPath := filepath.Join(dir, "photo.heic")
	out, err := exec.Command("sips", "-s", "format", "heic", pngPath, "--out", heicPath).CombinedOutput()
	if err != nil {
		t.Skipf("could not create heic fixture: %v (%s)", err, out)
	}

	jpegPath, err := preview.JPEGFor(dir, heicPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(jpegPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected cached jpeg, got %v %v", info, err)
	}

	jpegPath2, err := preview.JPEGFor(dir, heicPath)
	if err != nil {
		t.Fatal(err)
	}
	if jpegPath2 != jpegPath {
		t.Fatalf("cache path changed: %s vs %s", jpegPath, jpegPath2)
	}
}
