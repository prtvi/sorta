package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

// cachePath returns the on-disk path for a resized preview without creating directories.
// Safe for existence checks during warmup inspection.
func cachePath(root, srcPath string, size Size) (dest, key string, err error) {
	_, suffix := sizeParams(size)
	key, err = cacheKey(srcPath, suffix)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(root, ".sorta", "cache", key+".jpg"), key, nil
}

// cacheDest returns the on-disk path for a resized preview of srcPath and ensures
// the cache directory exists (for writers).
func cacheDest(root, srcPath string, size Size) (dest, key string, err error) {
	dest, key, err = cachePath(root, srcPath, size)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", "", fmt.Errorf("create cache: %w", err)
	}
	return dest, key, nil
}

// freshCache reports whether dest is a non-empty cached preview not older than src.
func freshCache(srcPath, dest string) bool {
	info, err := os.Stat(dest)
	if err != nil || info.Size() == 0 {
		return false
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return false
	}
	return !srcInfo.ModTime().After(info.ModTime())
}

// HasCachedJPEG reports whether a valid resized JPEG already exists for srcPath.
func HasCachedJPEG(root, srcPath string, size Size) bool {
	dest, _, err := cachePath(root, srcPath, size)
	if err != nil {
		return false
	}
	return freshCache(srcPath, dest)
}

// DisplayJPEG returns a cached resized JPEG for UI display. Originals are never modified.
func DisplayJPEG(root, srcPath string, size Size) (string, error) {
	edge, _ := sizeParams(size)
	dest, key, err := cacheDest(root, srcPath, size)
	if err != nil {
		return "", err
	}

	lock := lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	if freshCache(srcPath, dest) {
		return dest, nil
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
	// Basename + size + mtime so previews survive classification moves
	// (root → liked) without regenerating from the original again.
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%d|%d|%s", filepath.Base(srcPath), info.Size(), info.ModTime().UnixNano(), suffix)
	return hex.EncodeToString(h.Sum(nil))[:32] + "-" + suffix, nil
}

// WarmWorkers returns how many parallel convert jobs to run.
// sips/ImageMagick are process-heavy; oversubscribe a little for I/O wait.
func WarmWorkers() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	w := n * 2
	if w < 4 {
		w = 4
	}
	if w > 16 {
		w = 16
	}
	return w
}

// EnsureBoth builds the view preview from the original, then the thumb from
// that view JPEG so large HEIC/JPEG originals are decoded only once.
func EnsureBoth(root, srcPath string) (thumbPath, viewPath string, err error) {
	viewPath, err = DisplayJPEG(root, srcPath, SizeView)
	if err != nil {
		return "", "", err
	}

	thumbPath, thumbKey, err := cacheDest(root, srcPath, SizeThumb)
	if err != nil {
		return "", viewPath, err
	}
	lock := lockFor(thumbKey)
	lock.Lock()
	defer lock.Unlock()

	if freshCache(srcPath, thumbPath) {
		return thumbPath, viewPath, nil
	}
	// Resize from the already-decoded view JPEG — much cheaper than the original.
	if err := convertResizedJPEG(viewPath, thumbPath, thumbMaxEdge); err != nil {
		_ = os.Remove(thumbPath)
		return "", viewPath, err
	}
	return thumbPath, viewPath, nil
}

// NeedsBoth reports whether thumb and/or view caches are missing for srcPath.
func NeedsBoth(root, srcPath string) bool {
	return !HasCachedJPEG(root, srcPath, SizeThumb) || !HasCachedJPEG(root, srcPath, SizeView)
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
