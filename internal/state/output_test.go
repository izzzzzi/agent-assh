package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputStoreWriteAndReadPage(t *testing.T) {
	store := NewOutputStore(t.TempDir())

	if err := store.Write("abcdef12", []byte("a\nb\nc\n"), []byte("err\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	page, err := store.Read("abcdef12", "stdout", 1, 1)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if page.Content != "b\n" {
		t.Fatalf("Content = %q, want %q", page.Content, "b\n")
	}
	if page.TotalLines != 3 {
		t.Fatalf("TotalLines = %d, want 3", page.TotalLines)
	}
	if !page.HasMore {
		t.Fatalf("HasMore = false, want true")
	}
}

func TestOutputStoreRejectsBadID(t *testing.T) {
	store := NewOutputStore(t.TempDir())

	if _, err := store.Read("../bad", "stdout", 0, 1); err == nil {
		t.Fatalf("Read() error = nil, want error")
	}
}

func TestOutputStoreWriteRestrictsExistingFileModes(t *testing.T) {
	dir := t.TempDir()
	store := NewOutputStore(dir)
	id := "abcdef12"

	for _, name := range []string{id, id + ".err"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("old\n"), 0o666); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
		if err := os.Chmod(path, 0o666); err != nil {
			t.Fatalf("Chmod(%q) error = %v", path, err)
		}
	}

	if err := store.Write(id, []byte("out\n"), []byte("err\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	for _, name := range []string{id, id + ".err"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode for %q = %v, want %v", path, got, os.FileMode(0o600))
		}
	}
}
