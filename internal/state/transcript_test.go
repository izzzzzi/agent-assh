package state

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const transcriptSID = "abcdef12"

// transcriptTimestamp matches the RFC3339 stamp in the transcript header, which
// changes on every run and therefore cannot be compared literally.
var transcriptTimestamp = regexp.MustCompile(`ts=\S+`)

func readTranscript(t *testing.T, baseDir, sid string) string {
	t.Helper()
	path := filepath.Join(baseDir, "transcripts", sid, "transcript.log")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return transcriptTimestamp.ReplaceAllString(string(body), "ts=<ts>")
}

func TestTranscriptStoreAppendWritesCommandAndStreams(t *testing.T) {
	base := t.TempDir()
	store := NewTranscriptStore(base)

	if err := store.Append(transcriptSID, 3, "ls -la", []byte("a\n"), []byte("warn\n")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	want := "=== seq=3 ts=<ts> ===\n$ ls -la\na\n[stderr]\nwarn\n"
	if got := readTranscript(t, base, transcriptSID); got != want {
		t.Fatalf("transcript = %q, want %q", got, want)
	}
}

func TestTranscriptStoreAppendTerminatesUnterminatedStreams(t *testing.T) {
	base := t.TempDir()
	store := NewTranscriptStore(base)

	// Neither stream ends in a newline: Append must add one so the next
	// record cannot be glued onto the same line.
	if err := store.Append(transcriptSID, 1, "cmd", []byte("no-newline"), []byte("err")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	want := "=== seq=1 ts=<ts> ===\n$ cmd\nno-newline\n[stderr]\nerr\n"
	if got := readTranscript(t, base, transcriptSID); got != want {
		t.Fatalf("transcript = %q, want %q", got, want)
	}
}

func TestTranscriptStoreAppendOmitsEmptyStreams(t *testing.T) {
	base := t.TempDir()
	store := NewTranscriptStore(base)

	if err := store.Append(transcriptSID, 7, "echo hi", nil, nil); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	body := readTranscript(t, base, transcriptSID)
	if want := "=== seq=7 ts=<ts> ===\n$ echo hi\n"; body != want {
		t.Fatalf("transcript = %q, want %q", body, want)
	}
	if strings.Contains(body, "[stderr]") {
		t.Fatalf("transcript = %q, must not emit a stderr marker for empty stderr", body)
	}
}

func TestTranscriptStoreAppendKeepsNumericSeqVerbatim(t *testing.T) {
	base := t.TempDir()
	store := NewTranscriptStore(base)

	// Regression guard: the header used to be rendered by a hand-rolled int
	// formatter that silently produced an empty string for negative input,
	// turning the header into "seq=" with the value lost.
	if err := store.Append(transcriptSID, -1, "cmd", nil, nil); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if body := readTranscript(t, base, transcriptSID); !strings.Contains(body, "seq=-1 ") {
		t.Fatalf("transcript = %q, want the seq rendered as -1", body)
	}
}

func TestTranscriptStoreAppendAccumulatesRecords(t *testing.T) {
	base := t.TempDir()
	store := NewTranscriptStore(base)

	for _, seq := range []int{1, 2} {
		if err := store.Append(transcriptSID, seq, "cmd", nil, nil); err != nil {
			t.Fatalf("Append(seq=%d) error = %v", seq, err)
		}
	}

	body := readTranscript(t, base, transcriptSID)
	first := strings.Index(body, "seq=1 ")
	second := strings.Index(body, "seq=2 ")
	if first == -1 || second == -1 {
		t.Fatalf("transcript = %q, want both records", body)
	}
	if first > second {
		t.Fatalf("transcript = %q, want the earlier record first", body)
	}
	if count := strings.Count(body, "$ cmd"); count != 2 {
		t.Fatalf("transcript = %q, want 2 command lines, got %d", body, count)
	}
}

func TestTranscriptStoreAppendRejectsInvalidSID(t *testing.T) {
	base := t.TempDir()
	store := NewTranscriptStore(base)

	if err := store.Append("../escape", 1, "cmd", nil, nil); err == nil {
		t.Fatal("Append() error = nil, want invalid session id")
	}
	if err := store.Append("NOTHEX!!", 1, "cmd", nil, nil); err == nil {
		t.Fatal("Append() error = nil, want invalid session id")
	}

	// The SID is a path segment, so rejection must happen before anything is
	// created on disk.
	if _, err := os.Stat(filepath.Join(base, "transcripts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat(transcripts) error = %v, want the directory to be absent", err)
	}
}

func TestTranscriptStoreAppendUsesPrivateModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode assertions do not apply on Windows")
	}

	base := t.TempDir()
	store := NewTranscriptStore(base)
	if err := store.Append(transcriptSID, 1, "cmd", []byte("out\n"), nil); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	sidDir := filepath.Join(base, "transcripts", transcriptSID)
	dirInfo, err := os.Stat(sidDir)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", sidDir, err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %v, want %v", got, os.FileMode(0o700))
	}

	logInfo, err := os.Stat(filepath.Join(sidDir, "transcript.log"))
	if err != nil {
		t.Fatalf("Stat(transcript.log) error = %v", err)
	}
	if got := logInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("log mode = %v, want %v", got, os.FileMode(0o600))
	}
}

func TestTranscriptStoreAppendReportsDirectoryCreationFailure(t *testing.T) {
	store := NewTranscriptStore(filepath.Join(blockingFilePath(t), "transcripts"))

	if err := store.Append(transcriptSID, 1, "cmd", nil, nil); err == nil {
		t.Fatal("Append() error = nil, want a directory creation error")
	}
}

// TestTranscriptStoreAppendReportsOpenFailure covers the branch where the
// session directory exists but the log path cannot be opened — here because a
// directory already occupies it.
func TestTranscriptStoreAppendReportsOpenFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory cannot be opened for writing on POSIX")
	}

	base := t.TempDir()
	sidDir := filepath.Join(base, "transcripts", transcriptSID)
	if err := os.MkdirAll(filepath.Join(sidDir, "transcript.log"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	store := NewTranscriptStore(base)
	if err := store.Append(transcriptSID, 1, "cmd", nil, nil); err == nil {
		t.Fatal("Append() error = nil, want an open error")
	}
}
