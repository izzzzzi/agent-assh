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
	if !page.OK {
		t.Fatalf("OK = false, want true")
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
	if !page.OK {
		t.Fatalf("OK = false, want true")
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

func TestOutputStoreReadPaginationBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		offset      int
		limit       int
		wantLines   int
		wantLimit   int
		wantHasMore bool
		wantContent string
	}{
		{
			name: "final line without newline still counts",
			// A file whose last line lacks "\n" is the common shape for
			// command output; the line must still be addressable.
			content: "a\nb\nc", offset: 0, limit: 10,
			wantLines: 3, wantLimit: 3, wantContent: "a\nb\nc",
		},
		{
			name:    "offset equal to total yields an empty page",
			content: "a\nb\n", offset: 2, limit: 5,
			wantLines: 2, wantLimit: 5, wantContent: "",
		},
		{
			name:    "limit equal to remaining stops pagination",
			content: "a\nb\nc\n", offset: 1, limit: 2,
			wantLines: 3, wantLimit: 2, wantHasMore: false, wantContent: "b\nc\n",
		},
		{
			name:    "partial window reports more",
			content: "a\nb\nc\n", offset: 0, limit: 1,
			wantLines: 3, wantLimit: 1, wantHasMore: true, wantContent: "a\n",
		},
		{
			name:    "empty file has zero lines",
			content: "", offset: 0, limit: 5,
			wantLines: 0, wantLimit: 5, wantContent: "",
		},
		{
			name:    "blank line is a line of its own",
			content: "a\n\nb\n", offset: 1, limit: 1,
			wantLines: 3, wantLimit: 1, wantHasMore: true, wantContent: "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewOutputStore(t.TempDir())
			if err := store.Write("abcdef12", []byte(tt.content), []byte{}); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			page, err := store.Read("abcdef12", "stdout", tt.offset, tt.limit)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if !page.OK {
				t.Fatal("OK = false, want true")
			}
			if page.TotalLines != tt.wantLines {
				t.Fatalf("TotalLines = %d, want %d", page.TotalLines, tt.wantLines)
			}
			if page.Limit != tt.wantLimit {
				t.Fatalf("Limit = %d, want %d", page.Limit, tt.wantLimit)
			}
			if page.HasMore != tt.wantHasMore {
				t.Fatalf("HasMore = %v, want %v", page.HasMore, tt.wantHasMore)
			}
			if page.Content != tt.wantContent {
				t.Fatalf("Content = %q, want %q", page.Content, tt.wantContent)
			}
			if page.Offset != tt.offset {
				t.Fatalf("Offset = %d, want %d", page.Offset, tt.offset)
			}
		})
	}
}

func TestOutputStoreReadRejectsInvalidArguments(t *testing.T) {
	store := NewOutputStore(t.TempDir())
	if err := store.Write("abcdef12", []byte("a\n"), []byte{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	tests := []struct {
		name   string
		stream string
		offset int
		limit  int
	}{
		{"invalid stream", "combined", 0, 1},
		{"negative offset", "stdout", -1, 1},
		{"zero limit", "stdout", 0, 0},
		{"negative limit", "stdout", 0, -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := store.Read("abcdef12", tt.stream, tt.offset, tt.limit); err == nil {
				t.Fatalf("Read(%q, %d, %d) error = nil, want error", tt.stream, tt.offset, tt.limit)
			}
		})
	}
}

func TestOutputStoreReadMissingFileFails(t *testing.T) {
	store := NewOutputStore(t.TempDir())

	if _, err := store.Read("abcdef12", "stdout", 0, 1); err == nil {
		t.Fatal("Read() error = nil, want a missing-file error")
	}
}

func TestOutputStoreWriteRejectsInvalidID(t *testing.T) {
	store := NewOutputStore(t.TempDir())

	if err := store.Write("../escape", []byte("x"), []byte{}); err == nil {
		t.Fatal("Write() error = nil, want invalid output id")
	}
}

func TestOutputStoreWriteReportsDirectoryCreationFailure(t *testing.T) {
	store := NewOutputStore(filepath.Join(blockingFilePath(t), "outputs"))

	if err := store.Write("abcdef12", []byte("out\n"), []byte{}); err == nil {
		t.Fatal("Write() error = nil, want a directory creation error")
	}
}

// blockingFilePath returns the path of a regular file, so joining any further
// path segment onto it is guaranteed to fail: MkdirAll cannot descend through
// a file. Shared by the store tests that assert error propagation.
func blockingFilePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func TestOutputStoreWriteCreatesPrivateDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode assertions do not apply on Windows")
	}

	dir := filepath.Join(t.TempDir(), "nested", "outputs")
	store := NewOutputStore(dir)

	if err := store.Write("abcdef12", []byte("out\n"), []byte("err\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", dir, err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %v, want %v", got, os.FileMode(0o700))
	}
}

// TestWritePrivateFileReplacesDirectoryAtDestination covers the rename retry:
// renaming a regular file onto a directory fails, so writePrivateFile removes
// the obstacle and retries instead of leaving the destination unwritten.
func TestWritePrivateFileReplacesDirectoryAtDestination(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "abcdef12")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	if err := writePrivateFile(destination, []byte("out\n")); err != nil {
		t.Fatalf("writePrivateFile() error = %v", err)
	}

	info, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("destination mode = %v, want a regular file", info.Mode())
	}

	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "out\n" {
		t.Fatalf("destination content = %q, want %q", body, "out\n")
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"empty input has no lines", "", nil},
		{"single line without newline", "a", []string{"a"}},
		{"single terminated line", "a\n", []string{"a\n"}},
		{"terminated lines keep their newline", "a\nb\n", []string{"a\n", "b\n"}},
		{"unterminated final line is kept", "a\nb", []string{"a\n", "b"}},
		{"blank line counts", "a\n\nb\n", []string{"a\n", "\n", "b\n"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitLines(tt.content); !slicesEqual(got, tt.want) {
				t.Fatalf("splitLines(%q) = %#v, want %#v", tt.content, got, tt.want)
			}
		})
	}
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
