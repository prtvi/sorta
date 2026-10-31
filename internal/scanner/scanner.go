package scanner

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prtvi/sorta/internal/exifmeta"
	"github.com/prtvi/sorta/internal/models"
)

// Classification directories that must not be scanned for photos in the root.
var skipDirs = map[string]bool{
	"liked":       true,
	"shortlisted": true,
	"posting":     true,
	"sharing":     true,
	"disliked":    true,
	"posted":      true,
	"review":      true, // legacy folder name; ignored if present
	"deleted":     true, // legacy folder name; ignored if present
	".sorta":      true,
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
// Files inside liked/, shortlisted/, posting/, sharing/, disliked/, posted/, and .sorta/ are ignored.
// Leftover review/ or deleted/ dirs (legacy) are also skipped if present.
func ScanRoot(dir string) ([]models.Photo, error) {
	return scanDir(dir, "/photos/")
}

// ScanBucketDir lists supported images in an absolute directory with the given URL prefix.
func ScanBucketDir(dir, urlPrefix string) ([]models.Photo, error) {
	return scanDir(dir, urlPrefix)
}

type timedPhoto struct {
	name    string
	t       time.Time // sort key: EXIF capture time, else file mod time
	takenAt string    // set only when EXIF/capture metadata is available
	modTime time.Time
}

func scanDir(dir, urlPrefix string) ([]models.Photo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Photo{}, nil
		}
		return nil, err
	}

	items := make([]timedPhoto, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !IsSupportedImage(name) {
			continue
		}
		tp := timedPhoto{name: name}
		if info, err := entry.Info(); err == nil {
			tp.modTime = info.ModTime()
		}
		items = append(items, tp)
	}

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			path := filepath.Join(dir, items[i].name)
			if t, ok := exifmeta.CaptureTime(path); ok {
				items[i].t = t
				items[i].takenAt = t.Format("2006-01-02 15:04:05")
				return
			}
			items[i].t = items[i].modTime
		}(i)
	}
	wg.Wait()

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		aOK := !a.t.IsZero()
		bOK := !b.t.IsZero()
		switch {
		case aOK && bOK:
			if !a.t.Equal(b.t) {
				return a.t.Before(b.t)
			}
			return a.name < b.name
		case aOK && !bOK:
			return true
		case !aOK && bOK:
			return false
		default:
			return a.name < b.name
		}
	})

	photos := make([]models.Photo, 0, len(items))
	for _, it := range items {
		p := models.Photo{
			Name: it.name,
			URL:  urlPrefix + it.name,
		}
		if it.takenAt != "" {
			p.TakenAt = it.takenAt
		}
		photos = append(photos, p)
	}
	return photos, nil
}

// ListImageNames lists supported image basenames in dir (non-recursive), no EXIF.
// Faster than ScanRoot/ScanBucketDir when only paths are needed (e.g. preview warmup).
func ListImageNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if IsSupportedImage(name) {
			names = append(names, name)
		}
	}
	return names, nil
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

// BytesInDir sums the size of all regular files directly inside dir (non-recursive).
// Subdirectories (e.g. classification folders under root) are skipped.
func BytesInDir(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total, nil
}

// BytesInTree sums the size of all regular files under dir (recursive).
// The .sorta cache directory is skipped.
func BytesInTree(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if path != dir && strings.EqualFold(d.Name(), ".sorta") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return total, nil
}

// IsSkipDir reports whether name is a classification subdirectory.
func IsSkipDir(name string) bool {
	return skipDirs[strings.ToLower(name)]
}
