package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/transport"
)

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
	if got["ok"] != true || got["sid"] != "abcdef12" || got["seq"] != float64(2) || got["content"] != "hello\n" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["total_lines"] != float64(1) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionReadAcceptsJumpHost(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, command transport.SSHCommand, _ string) transport.Result {
		if command.Jump != "bastion.example.com" {
			t.Fatalf("command.Jump=%q want bastion.example.com", command.Jump)
		}
		return transport.Result{Stdout: []byte("hello\n\n__ASSH_TOTAL_LINES__=1\n"), ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{
		"session", "read", "--sid", "abcdef12",
		"--seq", "1", "--jump", "bastion.example.com",
	})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionReadRawPrintsOnlyContent(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	entry := session.RegistryEntry{
		SID:           "abcdef12",
		Label:         "deploy",
		Host:          "example.com",
		User:          "root",
		Port:          22,
		HostKeyPolicy: "accept-new",
		TmuxName:      "assh_abcdef12",
		CreatedAt:     time.Now().UTC(),
		TTLSeconds:    3600,
	}
	if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stdout:   []byte("line-a\nline-b\n\n__ASSH_TOTAL_LINES__=2\n"),
			ExitCode: 0,
		}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"session", "read", "--sid", "abcdef12", "--seq", "1", "--raw"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); got != "line-a\nline-b\n" {
		t.Fatalf("raw session output = %q", got)
	}
}

func TestSessionReadAllowsMarkerInUserContent(t *testing.T) {
	content, total, notFound := parseSessionRead([]byte("__ASSH_NOT_FOUND__\n\n__ASSH_TOTAL_LINES__=1\n"))
	if notFound {
		t.Fatalf("parseSessionRead treated user content as missing output")
	}
	if content != "__ASSH_NOT_FOUND__\n" || total != 1 {
		t.Fatalf("content=%q total=%d", content, total)
	}
}

func TestSessionReadRawNotFoundReturnsJSONError(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stdout:   []byte("__ASSH_NOT_FOUND__\n"),
			ExitCode: 0,
		}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"session", "read", "--sid", "abcdef12", "--seq", "1", "--raw"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != false || got["error"] != "output_not_found" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if strings.Contains(out.String(), "__ASSH_NOT_FOUND__") {
		t.Fatalf("raw not-found marker leaked to output: %q", out.String())
	}
}

func TestSessionReadRawRemoteFailureReturnsJSONError(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stderr:   []byte("remote read failed"),
			ExitCode: 1,
			Err:      &exec.ExitError{},
		}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"session", "read", "--sid", "abcdef12", "--seq", "1", "--raw"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != false || got["error"] != "command_failed" {
		t.Fatalf("unexpected response: %#v", got)
	}
}
