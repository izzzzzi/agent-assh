package state

import (
	"os"
	"path/filepath"
	"runtime"
)

// BaseDir returns the directory holding assh's local state. The platform
// switch is delegated to baseDir so every branch can be tested on any host.
func BaseDir() string {
	return baseDir(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

// baseDir resolves the state directory for an explicit GOOS and environment.
// Splitting it out of BaseDir keeps the Windows and Linux branches testable on
// a macOS or Linux developer machine, where they would otherwise never run.
func baseDir(goos string, getenv func(string) string, homeDir func() (string, error)) string {
	if override := getenv("ASSH_STATE_DIR"); override != "" {
		return override
	}
	switch goos {
	case "windows":
		if localAppData := getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "assh")
		}
	case "darwin":
		if home, err := homeDir(); err == nil && home != "" {
			return filepath.Join(home, "Library", "Application Support", "assh")
		}
	default:
		if stateHome := getenv("XDG_STATE_HOME"); stateHome != "" {
			return filepath.Join(stateHome, "assh")
		}
		if home, err := homeDir(); err == nil && home != "" {
			return filepath.Join(home, ".local", "state", "assh")
		}
	}
	return "assh"
}
