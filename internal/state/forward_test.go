package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestForwardStoreSaveLoadListDelete(t *testing.T) {
	store := NewForwardStore(t.TempDir())
	created := time.Now().UTC().Truncate(time.Second)
	record := ForwardRecord{
		Name:             "deploy",
		Host:             "example.com",
		User:             "root",
		Port:             22,
		Identity:         "key",
		Jump:             "bastion.example.com",
		Local:            []string{"127.0.0.1:8080:127.0.0.1:80"},
		Remote:           []string{"9000:127.0.0.1:9000"},
		Dynamic:          []string{"127.0.0.1:1080"},
		ControlSocket:    filepath.Join(t.TempDir(), "deploy.sock"),
		CreatedAtRFC3339: created.Format(time.RFC3339),
		PersistSeconds:   3600,
	}

	if err := store.Save(record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load("deploy")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Name != record.Name || loaded.Host != record.Host {
		t.Fatalf("loaded record = %#v, want %#v", loaded, record)
	}
	if loaded.Jump != record.Jump || loaded.ControlSocket != record.ControlSocket {
		t.Fatalf("loaded record = %#v, want %#v", loaded, record)
	}
	if !slicesEqual(loaded.Local, record.Local) || !slicesEqual(loaded.Remote, record.Remote) {
		t.Fatalf("loaded rules = %#v", loaded)
	}
	if !slicesEqual(loaded.Dynamic, record.Dynamic) {
		t.Fatalf("loaded rules = %#v", loaded)
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].Name != "deploy" {
		t.Fatalf("records = %#v", records)
	}

	if err := store.Delete("deploy"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Load("deploy"); err == nil {
		t.Fatal("Load() error = nil after delete")
	}
}

func TestForwardStoreRejectsUnsafeNames(t *testing.T) {
	store := NewForwardStore(t.TempDir())
	if err := store.Save(ForwardRecord{Name: "../bad"}); err == nil {
		t.Fatal("Save() error = nil, want invalid name")
	}
	if _, err := store.Load("../bad"); err == nil {
		t.Fatal("Load() error = nil, want invalid name")
	}
}

func TestForwardStoreDeleteIsIdempotent(t *testing.T) {
	store := NewForwardStore(t.TempDir())

	// Deleting a record that was never saved is not an error: callers tear
	// down forwards without knowing whether the record still exists.
	if err := store.Delete("missing"); err != nil {
		t.Fatalf("Delete() error = %v, want nil for a missing record", err)
	}
	if err := store.Delete("../bad"); err == nil {
		t.Fatal("Delete() error = nil, want invalid name")
	}
}

func TestForwardStoreListMissingDirectoryIsEmpty(t *testing.T) {
	store := NewForwardStore(t.TempDir())

	records, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want none", records)
	}
}

func TestForwardStoreListSkipsNonJSONEntries(t *testing.T) {
	base := t.TempDir()
	store := NewForwardStore(base)
	if err := store.Save(ForwardRecord{Name: "deploy", Host: "example.com"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	dir := filepath.Join(base, "forward")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// A directory whose name ends in .json must be skipped, not read.
	if err := os.Mkdir(filepath.Join(dir, "nested.json"), 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].Name != "deploy" {
		t.Fatalf("records = %#v, want only deploy", records)
	}
}

func TestForwardStoreListSortsByName(t *testing.T) {
	store := NewForwardStore(t.TempDir())
	for _, name := range []string{"zeta", "alpha", "mid"} {
		if err := store.Save(ForwardRecord{Name: name, Host: "example.com"}); err != nil {
			t.Fatalf("Save(%q) error = %v", name, err)
		}
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	got := make([]string, 0, len(records))
	for _, record := range records {
		got = append(got, record.Name)
	}
	if !slicesEqual(got, []string{"alpha", "mid", "zeta"}) {
		t.Fatalf("List() = %#v, want sorted by name", got)
	}
}

func TestForwardStoreLoadRejectsCorruptRecord(t *testing.T) {
	base := t.TempDir()
	store := NewForwardStore(base)

	dir := filepath.Join(base, "forward")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deploy.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := store.Load("deploy"); err == nil {
		t.Fatal("Load() error = nil, want a decode error")
	}
}

func TestForwardStoreSaveCreatesPrivateDirectory(t *testing.T) {
	base := t.TempDir()
	store := NewForwardStore(base)
	if err := store.Save(ForwardRecord{Name: "deploy", Host: "example.com"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(base, "forward"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %v, want %v", got, os.FileMode(0o700))
	}
}

func TestForwardStoreSaveReportsDirectoryCreationFailure(t *testing.T) {
	store := NewForwardStore(filepath.Join(blockingFilePath(t), "forward"))

	if err := store.Save(ForwardRecord{Name: "deploy", Host: "example.com"}); err == nil {
		t.Fatal("Save() error = nil, want a directory creation error")
	}
}

// slicesEqual reports whether two string slices have equal length and equal
// elements. Used by TestForwardStoreSaveLoadListDelete to compare rule slices.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
