package state

import (
	"os"
	"path/filepath"
	"runtime"
)

func BaseDir() string {
	if override := os.Getenv("ASSH_STATE_DIR"); override != "" {
		return override
	}
	switch runtime.GOOS {
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "assh")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, "Library", "Application Support", "assh")
		}
	default:
		if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
			return filepath.Join(stateHome, "assh")
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".local", "state", "assh")
		}
	}
	return "assh"
}
