package favorites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/prtvi/sorta/internal/models"
)

const dirName = ".sorta"
const fileName = "favorites.json"

// Path returns the favorites.json path under root.
func Path(root string) string {
	return filepath.Join(root, dirName, fileName)
}

// Load reads starred filenames. Missing or corrupt files return empty state.
func Load(root string) (models.FavoritesState, error) {
	data, err := os.ReadFile(Path(root))
	if err != nil {
		if os.IsNotExist(err) {
			return models.FavoritesState{Names: []string{}}, nil
		}
		return models.FavoritesState{Names: []string{}}, nil
	}
	var st models.FavoritesState
	if err := json.Unmarshal(data, &st); err != nil {
		return models.FavoritesState{Names: []string{}}, nil
	}
	if st.Names == nil {
		st.Names = []string{}
	}
	return st, nil
}

// Save writes starred filenames. Creates .sorta if needed.
func Save(root string, st models.FavoritesState) error {
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if st.Names == nil {
		st.Names = []string{}
	}
	sort.Strings(st.Names)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path(root) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path(root))
}

// SetStarred adds or removes a filename from favorites.
func SetStarred(root, filename string, starred bool) (models.FavoritesState, error) {
	st, _ := Load(root)
	set := make(map[string]struct{}, len(st.Names))
	for _, n := range st.Names {
		if n != "" {
			set[n] = struct{}{}
		}
	}
	if starred {
		set[filename] = struct{}{}
	} else {
		delete(set, filename)
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	st.Names = names
	if err := Save(root, st); err != nil {
		return st, err
	}
	return st, nil
}

// IsStarred reports whether filename is in favorites.
func IsStarred(root, filename string) bool {
	st, _ := Load(root)
	for _, n := range st.Names {
		if n == filename {
			return true
		}
	}
	return false
}
