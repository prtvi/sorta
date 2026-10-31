package filesystem

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ErrDirectoryCancelled means the user dismissed the native folder picker.
var ErrDirectoryCancelled = errors.New("folder selection cancelled")

// ChooseDirectory opens a native folder picker and returns the selected path.
// Returns ErrDirectoryCancelled when the user dismisses the dialog.
func ChooseDirectory(prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		prompt = "Choose a photo folder for Sorta"
	}
	var path string
	var err error
	switch runtime.GOOS {
	case "darwin":
		path, err = chooseDirectoryDarwin(prompt)
	case "windows":
		path, err = chooseDirectoryWindows(prompt)
	default:
		path, err = chooseDirectoryLinux(prompt)
	}
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	path = strings.TrimRight(path, "/\\")
	if path == "" {
		return "", ErrDirectoryCancelled
	}
	return path, nil
}

func chooseDirectoryDarwin(prompt string) (string, error) {
	// Return a sentinel on cancel so we can distinguish it from real errors.
	script := fmt.Sprintf(`try
	POSIX path of (choose folder with prompt %q)
on error number -128
	return "__CANCELLED__"
end try`, prompt)
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("open folder picker: %w", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "__CANCELLED__" {
		return "", ErrDirectoryCancelled
	}
	return s, nil
}

func chooseDirectoryWindows(prompt string) (string, error) {
	// PowerShell FolderBrowserDialog; empty output means cancel.
	ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms | Out-Null
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = %q
$dialog.ShowNewFolderButton = $true
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
  [Console]::Out.Write($dialog.SelectedPath)
}
`, prompt)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("open folder picker: %w", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", ErrDirectoryCancelled
	}
	return s, nil
}

func chooseDirectoryLinux(prompt string) (string, error) {
	if path, err := runFolderPicker("zenity", "--file-selection", "--directory", "--title="+prompt); err == nil || errors.Is(err, ErrDirectoryCancelled) {
		return path, err
	}
	if path, err := runFolderPicker("kdialog", "--getexistingdirectory", ".", "--title", prompt); err == nil || errors.Is(err, ErrDirectoryCancelled) {
		return path, err
	}
	return "", fmt.Errorf("no folder picker found (install zenity or kdialog)")
}

func runFolderPicker(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// zenity/kdialog exit 1 on cancel
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.TrimSpace(stdout.String()) == "" {
			return "", ErrDirectoryCancelled
		}
		return "", fmt.Errorf("open folder picker (%s): %w", name, err)
	}
	s := strings.TrimSpace(stdout.String())
	if s == "" {
		return "", ErrDirectoryCancelled
	}
	return s, nil
}
