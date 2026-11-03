// Package meta owns the single on-disk metadata file at <root>/.sorta/meta.json.
// Session resume, favorites, and preview-warmup fingerprints all live here.
// Binary preview JPEGs remain under .sorta/cache/ (not JSON).
package meta

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/prtvi/sorta/internal/models"
)

const (
	dirName  = ".sorta"
	fileName = "meta.json"
	bakName  = "meta.json.bak"
)

// ErrCorrupt is returned when meta.json (and its backup) cannot be parsed.
// Callers must not treat this as an empty file — writing would wipe state.
var ErrCorrupt = errors.New("meta.json corrupt")

// WarmDoneEntry is a finished warmup fingerprint for a bucket+size (or directory).
type WarmDoneEntry struct {
	FP    string `json:"fp"`
	Total int    `json:"total"`
}

// File is the full contents of .sorta/meta.json.
type File struct {
	Session   models.SessionState      `json:"session"`
	Favorites models.FavoritesState    `json:"favorites"`
	WarmDone  map[string]WarmDoneEntry `json:"warmDone,omitempty"`
}

var (
	mu       sync.Mutex
	migrated = map[string]struct{}{} // roots already migrated this process
)

// Path returns <root>/.sorta/meta.json.
func Path(root string) string {
	return filepath.Join(root, dirName, fileName)
}

func bakPath(root string) string {
	return filepath.Join(root, dirName, bakName)
}

// Update loads meta (migrating legacy files once), applies fn, and writes atomically.
// Refuses to write when the existing file is corrupt and no backup can be restored,
// so a bad read cannot wipe session/favorites on the next save.
func Update(root string, fn func(*File) error) error {
	mu.Lock()
	defer mu.Unlock()

	f, err := loadLocked(root)
	if err != nil {
		return err
	}
	if err := fn(&f); err != nil {
		return err
	}
	return saveLocked(root, f)
}

// Read returns a copy of the meta file (migrating legacy files once).
// On corrupt primary with a good backup, restores from backup.
// On total corruption, returns empty data and ErrCorrupt (does not rewrite disk).
func Read(root string) (File, error) {
	mu.Lock()
	defer mu.Unlock()
	return loadLocked(root)
}

func loadLocked(root string) (File, error) {
	migrateLegacyLocked(root)

	data, err := os.ReadFile(Path(root))
	if err != nil {
		if os.IsNotExist(err) {
			return emptyFile(), nil
		}
		return File{}, err
	}
	f, err := parseFile(data)
	if err == nil {
		return f, nil
	}

	// Primary corrupt — try backup before giving up.
	bakData, bakErr := os.ReadFile(bakPath(root))
	if bakErr != nil {
		return File{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	bak, bakErr := parseFile(bakData)
	if bakErr != nil {
		return File{}, fmt.Errorf("%w: primary and backup unreadable", ErrCorrupt)
	}
	// Restore backup to primary so the next boot is clean.
	_ = saveLocked(root, bak)
	return bak, nil
}

func parseFile(data []byte) (File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, err
	}
	if f.Favorites.Names == nil {
		f.Favorites.Names = []string{}
	}
	if f.WarmDone == nil {
		f.WarmDone = map[string]WarmDoneEntry{}
	}
	return f, nil
}

func saveLocked(root string, f File) error {
	if f.Favorites.Names == nil {
		f.Favorites.Names = []string{}
	}
	if f.WarmDone == nil {
		f.WarmDone = map[string]WarmDoneEntry{}
	}
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dest := Path(root)
	// Keep previous good file as backup before replacing.
	if prev, err := os.ReadFile(dest); err == nil && json.Valid(prev) {
		_ = os.WriteFile(bakPath(root), prev, 0o644)
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func emptyFile() File {
	return File{
		Favorites: models.FavoritesState{Names: []string{}},
		WarmDone:  map[string]WarmDoneEntry{},
	}
}

// migrateLegacyLocked folds session.json / favorites.json / warm-done.json into
// meta.json once per root, then removes the legacy files.
func migrateLegacyLocked(root string) {
	if _, ok := migrated[root]; ok {
		return
	}
	migrated[root] = struct{}{}

	metaPath := Path(root)
	if _, err := os.Stat(metaPath); err == nil {
		// meta.json already exists — still scrub leftover legacy files.
		removeLegacy(root)
		return
	}

	f := emptyFile()
	changed := false

	if data, err := os.ReadFile(filepath.Join(root, dirName, "session.json")); err == nil {
		var st models.SessionState
		if json.Unmarshal(data, &st) == nil {
			f.Session = st
			changed = true
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, dirName, "favorites.json")); err == nil {
		var st models.FavoritesState
		if json.Unmarshal(data, &st) == nil {
			if st.Names == nil {
				st.Names = []string{}
			}
			f.Favorites = st
			changed = true
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, dirName, "warm-done.json")); err == nil {
		var m map[string]WarmDoneEntry
		if json.Unmarshal(data, &m) == nil && m != nil {
			f.WarmDone = m
			changed = true
		}
	}

	if changed {
		_ = saveLocked(root, f)
	}
	removeLegacy(root)
}

func removeLegacy(root string) {
	for _, name := range []string{"session.json", "favorites.json", "warm-done.json"} {
		_ = os.Remove(filepath.Join(root, dirName, name))
		_ = os.Remove(filepath.Join(root, dirName, name+".tmp"))
	}
}
