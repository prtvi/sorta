package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prtvi/sorta/internal/models"
	"github.com/prtvi/sorta/internal/scanner"
)

// ClassificationDirs are created under the working directory at startup.
var ClassificationDirs = []string{"liked", "disliked", "review", "deleted"}

// Store performs safe filesystem operations constrained to a root directory.
type Store struct {
	Root string
}

// NewStore validates root and returns a Store.
func NewStore(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", abs)
	}
	return &Store{Root: abs}, nil
}

// EnsureClassificationDirs creates liked/, disliked/, review/, and deleted/ if missing.
func (s *Store) EnsureClassificationDirs() error {
	for _, name := range ClassificationDirs {
		dir := filepath.Join(s.Root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
	}
	return nil
}

// SanitizeFilename rejects path separators and traversal segments.
// The browser must never be able to escape the working directory.
func SanitizeFilename(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty filename")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("invalid filename")
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("invalid filename")
	}
	cleaned := filepath.Base(name)
	if cleaned != name {
		return "", fmt.Errorf("invalid filename")
	}
	return cleaned, nil
}

// ResolveInRoot joins name under root and ensures the result stays inside root.
func (s *Store) ResolveInRoot(name string) (string, error) {
	safe, err := SanitizeFilename(name)
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.Root, safe)
	rel, err := filepath.Rel(s.Root, full)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes root")
	}
	return full, nil
}

// uniqueDestination returns dest if free, otherwise dest with " (n)" before the extension.
// Never overwrites an existing file.
func uniqueDestination(dest string) (string, error) {
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest, nil
	} else if err != nil {
		return "", err
	}

	dir := filepath.Dir(dest)
	base := filepath.Base(dest)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for i := 1; i < 10000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not find unique destination name")
}

// MoveToClassification moves a root-level photo into liked|disliked|review.
// Returns the ActionRecord needed for undo.
func (s *Store) MoveToClassification(filename string, action models.PhotoAction) (*models.ActionRecord, error) {
	return s.MoveBetweenBuckets(filename, models.BucketRoot, action.ToBucket())
}

// ResolveInBucket resolves a filename inside a bucket under the working root.
func (s *Store) ResolveInBucket(bucket models.Bucket, name string) (string, error) {
	if !bucket.Valid() {
		return "", fmt.Errorf("invalid bucket")
	}
	safe, err := SanitizeFilename(name)
	if err != nil {
		return "", err
	}
	var full string
	if bucket == models.BucketRoot {
		full = filepath.Join(s.Root, safe)
	} else {
		full = filepath.Join(s.Root, string(bucket), safe)
	}
	rel, err := filepath.Rel(s.Root, full)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	if strings.HasPrefix(rel, "..") || rel == ".." {
		return "", fmt.Errorf("path escapes root")
	}
	// Disallow accessing .sorta via crafted names (already sanitized to basename).
	if strings.HasPrefix(rel, ".sorta") {
		return "", fmt.Errorf("path escapes root")
	}
	return full, nil
}

// FindInLibrary locates a filename in root or any classification bucket.
func (s *Store) FindInLibrary(name string) (string, error) {
	for _, b := range []models.Bucket{
		models.BucketRoot,
		models.BucketLiked,
		models.BucketReview,
		models.BucketDisliked,
		models.BucketDeleted,
	} {
		path, err := s.ResolveInBucket(b, name)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

// BucketDir returns the absolute directory for a bucket.
func (s *Store) BucketDir(bucket models.Bucket) (string, error) {
	if !bucket.Valid() {
		return "", fmt.Errorf("invalid bucket")
	}
	if bucket == models.BucketRoot {
		return s.Root, nil
	}
	return filepath.Join(s.Root, string(bucket)), nil
}

// MoveBetweenBuckets moves a photo from one bucket to another using rename.
// Conflicts get a unique " (n)" suffix (existing V1 strategy — never overwrite).
// Matching Sony .ARW sidecars (same basename stem) move with the photo.
func (s *Store) MoveBetweenBuckets(filename string, from, to models.Bucket) (*models.ActionRecord, error) {
	if !from.Valid() || !to.Valid() {
		return nil, fmt.Errorf("invalid bucket")
	}
	if from == to {
		return nil, fmt.Errorf("source and destination are the same")
	}

	src, err := s.ResolveInBucket(from, filename)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}

	destDir, err := s.BucketDir(to)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}

	dest := filepath.Join(destDir, filepath.Base(src))
	dest, err = uniqueDestination(dest)
	if err != nil {
		return nil, err
	}

	arwSrc := findARWCompanion(src)

	if err := os.Rename(src, dest); err != nil {
		return nil, err
	}

	var companions []models.CompanionMove
	if arwSrc != "" {
		companionDest, cerr := companionDestination(destDir, dest, arwSrc)
		if cerr != nil {
			_ = os.Rename(dest, src)
			return nil, cerr
		}
		if err := os.Rename(arwSrc, companionDest); err != nil {
			_ = os.Rename(dest, src)
			return nil, err
		}
		companions = append(companions, models.CompanionMove{
			OriginalPath: arwSrc,
			Destination:  companionDest,
		})
	}

	action := models.PhotoAction(to)
	if to == models.BucketRoot {
		action = ""
	}

	return &models.ActionRecord{
		Filename:     filepath.Base(dest),
		OriginalPath: src,
		Destination:  dest,
		Action:       action,
		FromBucket:   from,
		ToBucket:     to,
		Companions:   companions,
	}, nil
}

// findARWCompanion returns the path of a same-stem .ARW sidecar next to src, or "".
func findARWCompanion(src string) string {
	if strings.EqualFold(filepath.Ext(src), ".arw") {
		return ""
	}
	dir := filepath.Dir(src)
	stem := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.EqualFold(filepath.Ext(name), ".arw") {
			continue
		}
		compStem := strings.TrimSuffix(name, filepath.Ext(name))
		if strings.EqualFold(compStem, stem) {
			return filepath.Join(dir, name)
		}
	}
	return ""
}

// companionDestination places the sidecar beside the moved photo, matching the
// photo's final stem (including any " (n)" conflict suffix) and preserving the
// sidecar's original extension casing.
func companionDestination(destDir, photoDest, companionSrc string) (string, error) {
	photoBase := filepath.Base(photoDest)
	photoStem := strings.TrimSuffix(photoBase, filepath.Ext(photoBase))
	ext := filepath.Ext(companionSrc)
	dest := filepath.Join(destDir, photoStem+ext)
	return uniqueDestination(dest)
}

// ListLibraryBucket returns photos in a bucket (root|liked|review|disliked|deleted).
func (s *Store) ListLibraryBucket(bucket models.Bucket) ([]models.Photo, error) {
	if !bucket.LibraryBucket() {
		return nil, fmt.Errorf("invalid library bucket")
	}
	dir, err := s.BucketDir(bucket)
	if err != nil {
		return nil, err
	}
	var prefix string
	if bucket == models.BucketRoot {
		prefix = "/photos/"
		photos, err := scanner.ScanRoot(dir)
		return photos, err
	}
	prefix = "/photos/" + string(bucket) + "/"
	return scanner.ScanBucketDir(dir, prefix)
}

// UndoMove restores a previously moved file to its original path.
// If the original name is taken, a unique name is chosen.
// Companion sidecars recorded on the action are restored first.
func (s *Store) UndoMove(rec models.ActionRecord) (string, error) {
	if _, err := os.Stat(rec.Destination); err != nil {
		return "", err
	}

	for _, c := range rec.Companions {
		if err := undoCompanion(s.Root, c); err != nil {
			return "", err
		}
	}

	dest := rec.OriginalPath
	rel, err := filepath.Rel(s.Root, dest)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("undo path escapes root")
	}

	dest, err = uniqueDestination(dest)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(rec.Destination, dest); err != nil {
		return "", err
	}
	return filepath.Base(dest), nil
}

func undoCompanion(root string, c models.CompanionMove) error {
	if _, err := os.Stat(c.Destination); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	dest := c.OriginalPath
	rel, err := filepath.Rel(root, dest)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("undo companion path escapes root")
	}
	dest, err = uniqueDestination(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.Rename(c.Destination, dest)
}

// CollectStats returns counts from the filesystem.
func (s *Store) CollectStats() (models.Stats, error) {
	remaining, err := scanner.CountInDir(s.Root)
	if err != nil {
		return models.Stats{}, err
	}
	liked, err := scanner.CountInDir(filepath.Join(s.Root, "liked"))
	if err != nil {
		return models.Stats{}, err
	}
	disliked, err := scanner.CountInDir(filepath.Join(s.Root, "disliked"))
	if err != nil {
		return models.Stats{}, err
	}
	review, err := scanner.CountInDir(filepath.Join(s.Root, "review"))
	if err != nil {
		return models.Stats{}, err
	}
	deleted, err := scanner.CountInDir(filepath.Join(s.Root, "deleted"))
	if err != nil {
		return models.Stats{}, err
	}
	return models.Stats{
		Total:     remaining + liked + disliked + review,
		Remaining: remaining,
		Root:      remaining,
		Liked:     liked,
		Disliked:  disliked,
		Review:    review,
		Deleted:   deleted,
	}, nil
}
