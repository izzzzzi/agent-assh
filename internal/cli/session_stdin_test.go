package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestSessionExecStdinUploadsBoundedPrivateFileAndCleansUp(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	payload := []byte("#!/bin/sh\nprintf 'hello\\n'\n\x00binary\n")

	oldPiped := stdinIsPiped
	t.Cleanup(func() { stdinIsPiped = oldPiped })
	stdinIsPiped = func() bool { return true }
	oldSCP := runSCP
	t.Cleanup(func() { runSCP = oldSCP })
	var uploadedPath, remotePath string
	runSCP = func(_ context.Context, command transport.SCPCommand, source, destination string,
		direction transport.SCPDirection) transport.Result {
		if direction != transport.Upload || !command.Batch {
			t.Fatalf("upload direction=%v batch=%v", direction, command.Batch)
		}
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("ReadFile(upload source) error = %v", err)
		}
		if !bytes.Equal(body, payload) {
			t.Fatalf("uploaded payload=%q want %q", body, payload)
		}
		info, err := os.Stat(source)
		if err != nil {
			t.Fatalf("Stat(upload source) error = %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("upload source mode=%o want 600", info.Mode().Perm())
		}
		uploadedPath, remotePath = source, destination
		return transport.Result{ExitCode: 0}
	}
	oldSSH := runSSH
	t.Cleanup(func() { runSSH = oldSSH })
	var calls int
	runSSH = func(_ context.Context, _ transport.SSHCommand, command string) transport.Result {
		calls++
		if calls == 1 {
			if !strings.Contains(command, "< ~/'") || !strings.Contains(command, remotePath[2:]) {
				t.Fatalf("session command does not redirect uploaded stdin with tilde expansion: %s", command)
			}
			return sessionExecSuccessResult()
		}
		if !strings.Contains(command, "rm -f ~/'") || !strings.Contains(command, remotePath[2:]) {
			t.Fatalf("cleanup command=%s", command)
		}
		return transport.Result{ExitCode: 0}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewReader(payload))
	cmd.SetArgs([]string{"session", "exec", "--sid", "abcdef12", "--stdin", "--", "bash", "-s"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, output=%s", err, out.String())
	}
	if uploadedPath == "" || remotePath == "" {
		t.Fatal("stdin upload was not called")
	}
	if _, err := os.Stat(uploadedPath); !os.IsNotExist(err) {
		t.Fatalf("local stdin temp still exists, stat err=%v", err)
	}
	if calls != 2 {
		t.Fatalf("runSSH calls=%d want session + cleanup", calls)
	}
}

func TestSessionExecStdinRequiresPipe(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldPiped := stdinIsPiped
	t.Cleanup(func() { stdinIsPiped = oldPiped })
	stdinIsPiped = func() bool { return false }

	got := executeSessionJSONError(t, []string{"session", "exec", "--sid", "abcdef12", "--stdin", "--", "cat"})
	if got["error"] != "invalid_args" || got["message"] != "--stdin requires piped input" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionExecStdinRejectsOversizedInput(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldPiped := stdinIsPiped
	t.Cleanup(func() { stdinIsPiped = oldPiped })
	stdinIsPiped = func() bool { return true }

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewReader(bytes.Repeat([]byte("x"), int(maxSessionStdinBytes+1))))
	cmd.SetArgs([]string{"session", "exec", "--sid", "abcdef12", "--stdin", "--", "cat"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error=nil want stdin_too_large")
	}
	if !strings.Contains(out.String(), `"error":"stdin_too_large"`) {
		t.Fatalf("output=%s", out.String())
	}
}
