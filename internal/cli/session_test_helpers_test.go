package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/transport"
)

// Shared helpers for session_*_test.go files in this package.
// Kept in a non-_test.go-named file? No — Go requires _test.go suffix for
// test-only code, so this lives here as session_test_helpers.go.

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

// setMockSSH puts a fake ssh on PATH that runs the given script. Used to drive
// the session lifecycle without a real server.
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

func writeExpiredSessionRegistry(t *testing.T, sid string) {
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
		CreatedAt:     time.Now().UTC().Add(-2 * time.Hour),
		TTLSeconds:    3600,
	}
	if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}
}

// sessionExecSuccessResult is the canonical transport.Result returned by a
// successful `session exec` remote command: rc 0, no stdout/stderr lines.
// Used to stub runSSH in tests without repeating the marker string.
func sessionExecSuccessResult() transport.Result {
	return transport.Result{
		Stdout:   []byte("__ASSH_RC__=0\n__ASSH_STDOUT_LINES__=0\n__ASSH_STDERR_LINES__=0\n"),
		ExitCode: 0,
	}
}
