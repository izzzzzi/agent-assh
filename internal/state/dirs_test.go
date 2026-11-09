package state

import (
	"errors"
	"path/filepath"
	"testing"
)

// baseDir is pure, so the whole platform matrix can be asserted from any host:
// the Windows and Linux branches never run on a macOS developer machine
// otherwise. The fixtures below are shared by the platform-specific tests.
type baseDirCase struct {
	name string
	goos string
	env  map[string]string
	home func() (string, error)
	want string
}

var (
	baseDirHome         = filepath.Join("home", "alice")
	baseDirLocalAppData = filepath.Join("C:", "Users", "alice", "AppData", "Local")
	baseDirXDGStateHome = filepath.Join("xdg", "state")
)

func baseDirHomeMissing() (string, error) { return "", errors.New("no home directory") }

func baseDirHomeAt(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

func assertBaseDir(t *testing.T, cases []baseDirCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			if got := baseDir(tt.goos, getenv, tt.home); got != tt.want {
				t.Fatalf("baseDir(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

func TestBaseDirOverrideWinsOnEveryPlatform(t *testing.T) {
	const override = "/custom"

	assertBaseDir(t, []baseDirCase{
		{
			name: "darwin",
			goos: "darwin",
			env:  map[string]string{"ASSH_STATE_DIR": override},
			home: baseDirHomeAt(baseDirHome),
			want: override,
		},
		{
			name: "linux",
			goos: "linux",
			env:  map[string]string{"ASSH_STATE_DIR": override, "XDG_STATE_HOME": baseDirXDGStateHome},
			home: baseDirHomeAt(baseDirHome),
			want: override,
		},
		{
			name: "windows",
			goos: "windows",
			env:  map[string]string{"ASSH_STATE_DIR": override, "LOCALAPPDATA": baseDirLocalAppData},
			home: baseDirHomeMissing,
			want: override,
		},
	})
}

func TestBaseDirDarwinDefaults(t *testing.T) {
	applicationSupport := filepath.Join(baseDirHome, "Library", "Application Support", "assh")

	assertBaseDir(t, []baseDirCase{
		{
			name: "uses Application Support",
			goos: "darwin",
			home: baseDirHomeAt(baseDirHome),
			want: applicationSupport,
		},
		{
			name: "falls back when home is unknown",
			goos: "darwin",
			home: baseDirHomeMissing,
			want: "assh",
		},
		{
			name: "ignores XDG_STATE_HOME",
			goos: "darwin",
			env:  map[string]string{"XDG_STATE_HOME": baseDirXDGStateHome},
			home: baseDirHomeAt(baseDirHome),
			want: applicationSupport,
		},
	})
}

func TestBaseDirWindowsDefaults(t *testing.T) {
	assertBaseDir(t, []baseDirCase{
		{
			name: "uses LOCALAPPDATA",
			goos: "windows",
			env:  map[string]string{"LOCALAPPDATA": baseDirLocalAppData},
			home: baseDirHomeMissing,
			want: filepath.Join(baseDirLocalAppData, "assh"),
		},
		{
			name: "falls back when LOCALAPPDATA is unset",
			goos: "windows",
			home: baseDirHomeMissing,
			want: "assh",
		},
	})
}

func TestBaseDirLinuxDefaults(t *testing.T) {
	assertBaseDir(t, []baseDirCase{
		{
			name: "uses XDG_STATE_HOME",
			goos: "linux",
			env:  map[string]string{"XDG_STATE_HOME": baseDirXDGStateHome},
			home: baseDirHomeAt(baseDirHome),
			want: filepath.Join(baseDirXDGStateHome, "assh"),
		},
		{
			name: "falls back to the home state directory",
			goos: "linux",
			home: baseDirHomeAt(baseDirHome),
			want: filepath.Join(baseDirHome, ".local", "state", "assh"),
		},
		{
			name: "falls back to a bare name when home is unknown",
			goos: "linux",
			home: baseDirHomeMissing,
			want: "assh",
		},
	})
}

func TestBaseDirHonoursEnvironmentOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "assh-state")
	t.Setenv("ASSH_STATE_DIR", override)

	if got := BaseDir(); got != override {
		t.Fatalf("BaseDir() = %q, want %q", got, override)
	}
}

func TestBaseDirReturnsANonEmptyPath(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", "")

	if got := BaseDir(); got == "" {
		t.Fatal("BaseDir() = \"\", want a usable state directory")
	}
}
