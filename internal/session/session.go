package session

import (
	"errors"

	"github.com/prtvi/sorta/internal/meta"
	"github.com/prtvi/sorta/internal/models"
)

// Path returns the unified meta.json path (session lives inside it).
func Path(root string) string {
	return meta.Path(root)
}

// Load reads session state from .sorta/meta.json.
// Corrupt meta without a usable backup yields empty state and nil error
// (same as missing) so the UI can still boot; Save will fail until fixed.
func Load(root string) (models.SessionState, error) {
	f, err := meta.Read(root)
	if err != nil {
		if errors.Is(err, meta.ErrCorrupt) {
			return models.SessionState{}, nil
		}
		return models.SessionState{}, err
	}
	return f.Session, nil
}

// Save writes session state into .sorta/meta.json.
func Save(root string, st models.SessionState) error {
	return meta.Update(root, func(f *meta.File) error {
		f.Session = st
		return nil
	})
}
