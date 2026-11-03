package favorites

import (
	"errors"
	"sort"

	"github.com/prtvi/sorta/internal/meta"
	"github.com/prtvi/sorta/internal/models"
)

// Path returns the unified meta.json path (favorites live inside it).
func Path(root string) string {
	return meta.Path(root)
}

// Load reads starred filenames from .sorta/meta.json.
func Load(root string) (models.FavoritesState, error) {
	f, err := meta.Read(root)
	if err != nil {
		if errors.Is(err, meta.ErrCorrupt) {
			return models.FavoritesState{Names: []string{}}, nil
		}
		return models.FavoritesState{Names: []string{}}, err
	}
	st := f.Favorites
	if st.Names == nil {
		st.Names = []string{}
	}
	return st, nil
}

// Save writes starred filenames into .sorta/meta.json.
func Save(root string, st models.FavoritesState) error {
	if st.Names == nil {
		st.Names = []string{}
	}
	sort.Strings(st.Names)
	return meta.Update(root, func(f *meta.File) error {
		f.Favorites = st
		return nil
	})
}

// SetStarred adds or removes a filename from favorites.
func SetStarred(root, filename string, starred bool) (models.FavoritesState, error) {
	var out models.FavoritesState
	err := meta.Update(root, func(f *meta.File) error {
		set := make(map[string]struct{}, len(f.Favorites.Names))
		for _, n := range f.Favorites.Names {
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
		sort.Strings(names)
		f.Favorites.Names = names
		out = f.Favorites
		return nil
	})
	if out.Names == nil {
		out.Names = []string{}
	}
	return out, err
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
