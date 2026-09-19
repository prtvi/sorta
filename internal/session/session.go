package session

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/prtvi/sorta/internal/models"
)

const dirName = ".sorta"
const fileName = "session.json"

// Path returns the session.json path under root.
func Path(root string) string {
	return filepath.Join(root, dirName, fileName)
}

// Load reads session state. Missing or corrupt files return empty state and nil error.
func Load(root string) (models.SessionState, error) {
	data, err := os.ReadFile(Path(root))
	if err != nil {
		if os.IsNotExist(err) {
			return models.SessionState{}, nil
		}
		return models.SessionState{}, nil
	}
	var st models.SessionState
	if err := json.Unmarshal(data, &st); err != nil {
		return models.SessionState{}, nil
	}
	return st, nil
}

// Save writes session state. Never fatals; creates .sorta if needed.
func Save(root string, st models.SessionState) error {
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
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
