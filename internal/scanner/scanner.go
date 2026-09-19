package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/prtvi/sorta/internal/models"
)

// Classification directories that must not be scanned for photos in the root.
var skipDirs = map[string]bool{
	"liked":         true,
	"disliked":      true,
	"review":        true,
	".sorta": true,
}

// supportedExts are case-insensitive image extensions for V1.
// HEIC/HEIF are listed here; the server transcodes them to JPEG for browsers.
var supportedExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
	".heic": true,
	".heif": true,
}

// IsSupportedImage reports whether name has a supported image extension.
func IsSupportedImage(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return supportedExts[ext]
}

// ScanRoot lists supported image files in the root of dir (non-recursive).
// Files inside liked/, disliked/, review/, and .sorta/ are ignored.
func ScanRoot(dir string) ([]models.Photo, error) {
	return scanDir(dir, "/photos/")
}

// ScanBucketDir lists supported images in an absolute directory with the given URL prefix.
func ScanBucketDir(dir, urlPrefix string) ([]models.Photo, error) {
	return scanDir(dir, urlPrefix)
}

func scanDir(dir, urlPrefix string) ([]models.Photo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Photo{}, nil
		}
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !IsSupportedImage(name) {
			continue
		}
		names = append(names, name)
	}

	sort.Strings(names)

	photos := make([]models.Photo, 0, len(names))
	for _, name := range names {
		photos = append(photos, models.Photo{
			Name: name,
			URL:  urlPrefix + name,
		})
	}
	return photos, nil
}

// CountInDir counts supported images directly inside dir (non-recursive).
func CountInDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if IsSupportedImage(entry.Name()) {
			count++
		}
	}
	return count, nil
}

// IsSkipDir reports whether name is a classification subdirectory.
func IsSkipDir(name string) bool {
	return skipDirs[strings.ToLower(name)]
}
