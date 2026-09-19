package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Size selects a display preview tier.
type Size string

const (
	// SizeThumb is for library grids (~480px long edge).
	SizeThumb Size = "thumb"
	// SizeView is for cull / lightbox viewing (~2048px long edge).
	SizeView Size = "view"
)

const (
	thumbMaxEdge = 480
	viewMaxEdge  = 2048
	jpegQuality  = 80
)

var genLocks sync.Map // cache key -> *sync.Mutex

// NeedsTranscode reports whether the file must be converted for browsers.
// Kept for callers; all formats now go through DisplayJPEG.
func NeedsTranscode(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".heic" || ext == ".heif"
}

// JPEGFor returns a cached viewer-sized JPEG (legacy helper for HEIC callers/tests).
func JPEGFor(root, srcPath string) (string, error) {
	return DisplayJPEG(root, srcPath, SizeView)
}

// DisplayJPEG returns a cached resized JPEG for UI display. Originals are never modified.
func DisplayJPEG(root, srcPath string, size Size) (string, error) {
	edge, suffix := sizeParams(size)
	cacheDir := filepath.Join(root, ".sorta", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache: %w", err)
	}

	key, err := cacheKey(srcPath, suffix)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(cacheDir, key+".jpg")

	lock := lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
		srcInfo, err := os.Stat(srcPath)
		if err == nil && !srcInfo.ModTime().After(info.ModTime()) {
			return dest, nil
		}
	}

	if err := convertResizedJPEG(srcPath, dest, edge); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return dest, nil
}

func sizeParams(size Size) (edge int, suffix string) {
	switch size {
	case SizeThumb:
		return thumbMaxEdge, "thumb"
	default:
		return viewMaxEdge, "view"
	}
}

func lockFor(key string) *sync.Mutex {
	v, _ := genLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func cacheKey(srcPath, suffix string) (string, error) {
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = io.WriteString(h, srcPath)
	_, _ = fmt.Fprintf(h, "|%d|%d|%s", info.Size(), info.ModTime().UnixNano(), suffix)
	return hex.EncodeToString(h.Sum(nil))[:32] + "-" + suffix, nil
}

func convertResizedJPEG(src, dest string, maxEdge int) error {
	tmp := dest + ".tmp"
	defer os.Remove(tmp)

	var err error
	switch runtime.GOOS {
	case "darwin":
		err = runSipsResize(src, tmp, maxEdge)
	default:
		err = runMagickResize(src, tmp, maxEdge)
		if err != nil && NeedsTranscode(src) {
			// HEIC without magick: convert then resize if possible.
			err = runHeifConvert(src, tmp)
			if err == nil {
				err = runMagickResize(tmp, tmp+".jpg", maxEdge)
				if err == nil {
					_ = os.Rename(tmp+".jpg", tmp)
				}
			}
		}
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func runSipsResize(src, dest string, maxEdge int) error {
	cmd := exec.Command(
		"sips",
		"-Z", fmt.Sprintf("%d", maxEdge),
		"-s", "format", "jpeg",
		"-s", "formatOptions", fmt.Sprintf("%d", jpegQuality),
		src,
		"--out", dest,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sips preview: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runHeifConvert(src, dest string) error {
	path, err := exec.LookPath("heif-convert")
	if err != nil {
		return err
	}
	cmd := exec.Command(path, src, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("heif-convert: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runMagickResize(src, dest string, maxEdge int) error {
	bin, err := exec.LookPath("magick")
	if err != nil {
		bin, err = exec.LookPath("convert")
		if err != nil {
			return fmt.Errorf("no image converter found (need sips on macOS, or ImageMagick)")
		}
	}
	geom := fmt.Sprintf("%dx%d>", maxEdge, maxEdge)
	cmd := exec.Command(bin, src, "-auto-orient", "-resize", geom, "-quality", fmt.Sprintf("%d", jpegQuality), dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("imagemagick preview: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ParseSize maps a query value to Size (default view).
func ParseSize(v string) Size {
	if strings.EqualFold(v, string(SizeThumb)) {
		return SizeThumb
	}
	return SizeView
}
