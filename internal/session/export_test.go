package session

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/audit"
	"github.com/izzzzzi/agent-assh/internal/state"
)

func TestSessionExportBuildsTarGzArchive(t *testing.T) {
	baseDir := t.TempDir()
	sid := "abcdef12"
	entry := RegistryEntry{
		SID:           sid,
		Label:         "deploy",
		Host:          "example.com",
		User:          "root",
		Port:          22,
		HostKeyPolicy: "accept-new",
		TmuxName:      "assh_" + sid,
		CreatedAt:     time.Now().UTC(),
		TTLSeconds:    3600,
		Seq:           2,
	}
	if err := SaveRegistry(baseDir, entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}
	if err := audit.Write(filepath.Join(baseDir, "audit", "audit.jsonl"), audit.Event{
		Timestamp:   time.Now().UTC(),
		Action:      "session_exec",
		Host:        "example.com",
		User:        "root",
		CommandHash: "hash",
		ExitCode:    0,
	}); err != nil {
		t.Fatalf("audit.Write() error = %v", err)
	}
	outputs := state.NewSessionOutputStore(baseDir)
	for _, page := range []state.SessionOutputPage{
		{SID: sid, Seq: 1, Stream: "stdout", Offset: 0, Limit: 50, TotalLines: 1, Content: "hello\n"},
		{SID: sid, Seq: 1, Stream: "stderr", Offset: 0, Limit: 50, TotalLines: 1, Content: "warn\n"},
	} {
		if err := outputs.Write(page); err != nil {
			t.Fatalf("outputs.Write() error = %v", err)
		}
	}
	archivePath := filepath.Join(baseDir, "exports", "session.tar.gz")

	result, err := Export(baseDir, sid, archivePath)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	if result.SID != sid || result.Session != "deploy" || result.Archive != archivePath || result.Bytes == 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	files := readTarGz(t, archivePath)
	for _, name := range []string{"manifest.json", "session.json", "audit.jsonl", "outputs/seq-1-stdout.json", "outputs/seq-1-stderr.json"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("archive missing %q; files=%v", name, keys(files))
		}
	}
	if !strings.Contains(files["manifest.json"], `"sid": "abcdef12"`) {
		t.Fatalf("manifest missing sid: %s", files["manifest.json"])
	}
	if !strings.Contains(files["session.json"], `"label": "deploy"`) {
		t.Fatalf("session.json missing registry entry: %s", files["session.json"])
	}
	if !strings.Contains(files["audit.jsonl"], `"action":"session_exec"`) {
		t.Fatalf("audit.jsonl missing session audit event: %s", files["audit.jsonl"])
	}
	if !strings.Contains(files["outputs/seq-1-stdout.json"], `"content": "hello\n"`) {
		t.Fatalf("stdout page missing content: %s", files["outputs/seq-1-stdout.json"])
	}
}

func TestSessionExportRejectsUnknownSession(t *testing.T) {
	baseDir := t.TempDir()

	_, err := Export(baseDir, "abcdef12", filepath.Join(baseDir, "missing.tar.gz"))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func readTarGz(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer gz.Close()

	files := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next() error = %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		files[header.Name] = string(body)
	}
	return files
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
