package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestSessionExportRejectsUnknownSession(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())

	got := executeSessionJSONError(t, []string{"session", "export", "--sid", "abcdef12", "--output", filepath.Join(t.TempDir(), "missing.tar.gz")})
	if got["ok"] != false || got["error"] != "session_not_found" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionExportIsLocalOnly(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	if err := state.NewSessionOutputStore(stateBaseDir()).Write(state.SessionOutputPage{
		SID:        "abcdef12",
		Seq:        1,
		Stream:     "stdout",
		Offset:     0,
		Limit:      50,
		TotalLines: 1,
		Content:    "hello\n",
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		t.Fatalf("runSSH called during local export")
		return transport.Result{}
	}

	archivePath := filepath.Join(t.TempDir(), "session.tar.gz")
	got := executeSessionJSON(t, []string{"session", "export", "--sid", "abcdef12", "--output", archivePath})
	if got["ok"] != true || got["sid"] != "abcdef12" || got["session"] != "deploy" || got["archive"] != archivePath {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["bytes"].(float64) == 0 {
		t.Fatalf("expected archive bytes: %#v", got)
	}
	files := got["included_files"].([]any)
	if len(files) == 0 {
		t.Fatalf("expected included files: %#v", got)
	}
}

func TestSessionReadCachesOutputForExport(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{Stdout: []byte("line-a\nline-b\n\n__ASSH_TOTAL_LINES__=2\n"), ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{"session", "read", "--sid", "abcdef12", "--seq", "1", "--stream", "stdout", "--limit", "50"})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
	pages, err := state.NewSessionOutputStore(stateBaseDir()).List("abcdef12")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(pages) != 1 || pages[0].Seq != 1 || pages[0].Stream != "stdout" || pages[0].Content != "line-a\nline-b\n" {
		t.Fatalf("unexpected cached pages: %#v", pages)
	}
}

func TestSessionReadCachesMultiplePagesForExport(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{Stdout: []byte("line-a\nline-b\nline-c\n\n__ASSH_TOTAL_LINES__=3\n"), ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{"session", "read", "--sid", "abcdef12", "--seq", "1", "--stream", "stdout", "--offset", "0", "--limit", "2"})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
	got = executeSessionJSON(t, []string{"session", "read", "--sid", "abcdef12", "--seq", "1", "--stream", "stdout", "--offset", "2", "--limit", "1"})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
	pages, err := state.NewSessionOutputStore(stateBaseDir()).List("abcdef12")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected two cached pages, got %#v", pages)
	}
	if pages[0].Offset != 0 || pages[1].Offset != 2 {
		t.Fatalf("unexpected cached page offsets: %#v", pages)
	}
}

func TestSessionExportArchiveContainsCachedRead(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	store := state.NewSessionOutputStore(stateBaseDir())
	for _, page := range []state.SessionOutputPage{
		{SID: "abcdef12", Seq: 1, Stream: "stdout", Offset: 0, Limit: 50, TotalLines: 3, Content: "hello\n"},
		{SID: "abcdef12", Seq: 1, Stream: "stdout", Offset: 2, Limit: 1, TotalLines: 3, Content: "world\n"},
	} {
		if err := store.Write(page); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	archivePath := filepath.Join(t.TempDir(), "session.tar.gz")

	got := executeSessionJSON(t, []string{"session", "export", "--sid", "abcdef12", "--output", archivePath})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}

	files := readCLITarGz(t, archivePath)
	if !strings.Contains(files["outputs/seq-1-stdout-offset-0-limit-50.json"], `"content": "hello\n"`) || !strings.Contains(files["outputs/seq-1-stdout-offset-2-limit-1.json"], `"content": "world\n"`) {
		t.Fatalf("archive missing cached outputs: %#v", files)
	}
}

func TestPromptManifestIncludesSessionExportCommand(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "assh session export -s SID --output session.tar.gz") {
		t.Fatalf("prompt manifest missing session export command in %s", out.String())
	}
}

func readCLITarGz(t *testing.T, path string) map[string]string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := map[string]string{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next() error = %v", err)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		files[header.Name] = string(content)
	}
	return files
}
