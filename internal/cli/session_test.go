package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/agent-ssh/assh/internal/session"
)

func TestSessionOpenRequiresHost(t *testing.T) {
	got := executeSessionJSONError(t, []string{"session", "open"})
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "host required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionOpenRejectsUnexpectedPositionalArgs(t *testing.T) {
	got := executeSessionJSONError(t, []string{"session", "open", "--host", "example.com", "extra"})
	if got["ok"] != false || got["error"] != "invalid_args" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionOpenReturnsPlaceholderJSON(t *testing.T) {
	setMockSSH(t, "exit 0\n")
	t.Setenv("ASSH_STATE_DIR", t.TempDir())

	got := executeSessionJSON(t, []string{"session", "open", "--host", "example.com", "--name", "deploy", "--install-tmux"})

	if got["ok"] != true || got["host"] != "example.com" || got["session"] != "deploy" || got["install_tmux"] != true || got["sid"] == "" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionCloseRequiresSID(t *testing.T) {
	got := executeSessionJSONError(t, []string{"session", "close"})
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "--sid is required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionExecRequiresSIDAndCommand(t *testing.T) {
	got := executeSessionJSONError(t, []string{"session", "exec", "--sid", "bad", "--", "pwd"})
	if got["ok"] != false || got["error"] != "invalid_args" {
		t.Fatalf("unexpected response: %#v", got)
	}

	got = executeSessionJSONError(t, []string{"session", "exec", "--sid", "abcdef12"})
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "command required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionExecReturnsJSON(t *testing.T) {
	setMockSSH(t, "exit 0\n")
	writeTestSessionRegistry(t, "abcdef12")

	got := executeSessionJSON(t, []string{"session", "exec", "--sid", "abcdef12", "--", "pwd"})
	if got["ok"] != true || got["sid"] != "abcdef12" || got["seq"] != float64(1) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionReadRequiresSIDAndSeq(t *testing.T) {
	got := executeSessionJSONError(t, []string{"session", "read", "--seq", "1"})
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "--sid is required" {
		t.Fatalf("unexpected response: %#v", got)
	}

	got = executeSessionJSONError(t, []string{"session", "read", "--sid", "abcdef12"})
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "--seq is required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionReadValidatesFlags(t *testing.T) {
	tests := [][]string{
		{"session", "read", "--sid", "abcdef12", "--seq", "1", "--stream", "bad"},
		{"session", "read", "--sid", "abcdef12", "--seq", "1", "--offset", "-1"},
		{"session", "read", "--sid", "abcdef12", "--seq", "1", "--limit", "0"},
		{"session", "read", "--sid", "abcdef12", "--seq", "1", "extra"},
	}

	for _, args := range tests {
		got := executeSessionJSONError(t, args)
		if got["ok"] != false || got["error"] != "invalid_args" {
			t.Fatalf("args %v unexpected response: %#v", args, got)
		}
	}
}

func TestSessionReadReturnsJSON(t *testing.T) {
	setMockSSH(t, "printf 'hello\\n\\n__ASSH_TOTAL_LINES__=1\\n'\n")
	writeTestSessionRegistry(t, "abcdef12")

	got := executeSessionJSON(t, []string{"session", "read", "--sid", "abcdef12", "--seq", "2"})
	if got["ok"] != true || got["sid"] != "abcdef12" || got["seq"] != float64(2) || got["content"] != "hello\n" || got["total_lines"] != float64(1) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionGCReturnsDryRunCandidates(t *testing.T) {
	got := executeSessionJSON(t, []string{"session", "gc"})

	candidates, ok := got["candidates"].([]any)
	if !ok {
		t.Fatalf("candidates = %#v, want JSON array", got["candidates"])
	}
	if got["ok"] != true || got["dry_run"] != true || len(candidates) != 0 {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func executeSessionJSON(t *testing.T, args []string) map[string]any {
	t.Helper()

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, output = %q", err, out.String())
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	return got
}

func executeSessionJSONError(t *testing.T, args []string) map[string]any {
	t.Helper()

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	return got
}

func setMockSSH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	name := "ssh"
	body := "#!/bin/sh\n" + script
	if runtime.GOOS == "windows" {
		name = "ssh.bat"
		body = "@echo off\r\n" + script
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("write mock ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeTestSessionRegistry(t *testing.T, sid string) {
	t.Helper()
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	entry := session.RegistryEntry{
		SID:           sid,
		Label:         "deploy",
		Host:          "example.com",
		User:          "root",
		Port:          22,
		HostKeyPolicy: "accept-new",
		TmuxName:      "assh_" + sid,
		CreatedAt:     time.Now().UTC(),
		TTLSeconds:    3600,
	}
	if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}
}
