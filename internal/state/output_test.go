package state

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestOutputStoreReadOffsetBeyondTotalWithHugeLimit(t *testing.T) {
	store := NewOutputStore(t.TempDir())
	if err := store.Write("abcdef12", []byte("a\nb\n"), []byte{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	page, err := store.Read("abcdef12", "stdout", 99, maxInt())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if page.Content != "" {
		t.Fatalf("Content = %q, want empty", page.Content)
	}
	if page.TotalLines != 2 {
		t.Fatalf("TotalLines = %d, want 2", page.TotalLines)
	}
	if page.HasMore {
		t.Fatalf("HasMore = true, want false")
	}
}

func TestOutputStoreReadHugeLimitIsBoundedToTotal(t *testing.T) {
	store := NewOutputStore(t.TempDir())
	if err := store.Write("abcdef12", []byte("a\nb\n"), []byte{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	page, err := store.Read("abcdef12", "stdout", 0, maxInt())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if page.Content != "a\nb\n" {
		t.Fatalf("Content = %q, want %q", page.Content, "a\nb\n")
	}
	if page.TotalLines != 2 {
		t.Fatalf("TotalLines = %d, want 2", page.TotalLines)
	}
	if page.HasMore {
		t.Fatalf("HasMore = true, want false")
	}
}

func TestOutputStoreWriteRestrictsExistingFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode assertions do not apply on Windows")
	}

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

func TestOutputStoreWriteReplacesDestinationSymlink(t *testing.T) {
	dir := t.TempDir()
	store := NewOutputStore(dir)
	id := "abcdef12"
	target := filepath.Join(dir, "target")
	destination := filepath.Join(dir, id)

	if err := os.WriteFile(target, []byte("target\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.Symlink(target, destination); err != nil {
		t.Skipf("symlink creation not permitted: %v", err)
	}

	if err := store.Write(id, []byte("out\n"), []byte("err\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	targetContent, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(target) error = %v", err)
	}
	if string(targetContent) != "target\n" {
		t.Fatalf("target content = %q, want %q", targetContent, "target\n")
	}

	info, err := os.Lstat(destination)
	if err != nil {
		t.Fatalf("Lstat(destination) error = %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("destination is still a symlink")
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("destination mode = %v, want regular file", info.Mode())
	}

	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("ReadFile(destination) error = %v", err)
	}
	if string(content) != "out\n" {
		t.Fatalf("destination content = %q, want %q", content, "out\n")
	}
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
