package filesystem

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// RevealInFileManager opens the system file manager and selects path.
// On macOS this is Finder (open -R); on Windows Explorer; on Linux the parent folder.
func RevealInFileManager(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer", "/select,", path)
	default:
		// Most Linux file managers lack a portable “select file” flag; open the folder.
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("reveal in file manager: %w", err)
	}
	return nil
}
