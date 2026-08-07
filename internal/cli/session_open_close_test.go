package cli

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/transport"
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

	args := []string{"session", "open", "--host", "example.com",
		"--name", "deploy", "--install-tmux"}
	got := executeSessionJSON(t, args)

	if got["ok"] != true || got["host"] != "example.com" || got["session"] != "deploy" || got["install_tmux"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["sid"] == "" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["tmux_name"] == "" || !strings.HasPrefix(got["tmux_name"].(string), "assh_") {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionOpenAcceptsJumpHost(t *testing.T) {
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, command transport.SSHCommand, _ string) transport.Result {
		if command.Jump != "bastion.example.com" {
			t.Fatalf("command.Jump=%q want bastion.example.com", command.Jump)
		}
		return transport.Result{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
	}
	t.Setenv("ASSH_STATE_DIR", t.TempDir())

	got := executeSessionJSON(t, []string{"session", "open", "--host", "example.com", "--jump", "bastion.example.com"})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionOpenInstallTmuxFailureReturnsStableError(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		if !strings.Contains(remoteCommand, "tmux_install_failed") {
			t.Fatalf("install command does not emit tmux_install_failed: %s", remoteCommand)
		}
		return transport.Result{
			Stderr:   []byte("tmux_install_failed\n"),
			ExitCode: 1,
			Err:      &exec.ExitError{},
		}
	}

	got := executeJSONError(t, []string{"session", "open", "--host", "example.com", "--install-tmux"})
	if got["error"] != "tmux_install_failed" {
		t.Fatalf("unexpected response: %#v", got)
	}

	entries, err := session.ListRegistry(stateBaseDir())
	if err != nil {
		t.Fatalf("ListRegistry() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("registry saved despite install failure: %#v", entries)
	}
}

func TestSessionOpenTmuxMissingDoesNotSaveRegistry(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stdout:   nil,
			Stderr:   []byte("tmux_missing\n"),
			ExitCode: 127,
			Err:      &exec.ExitError{},
		}
	}

	got := executeJSONError(t, []string{"session", "open", "--host", "example.com", "--name", "deploy"})
	if got["error"] != "tmux_missing" {
		t.Fatalf("unexpected response: %#v", got)
	}

	entries, err := session.ListRegistry(stateBaseDir())
	if err != nil {
		t.Fatalf("ListRegistry() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("registry saved despite remote failure: %#v", entries)
	}
}

func TestSessionCloseRequiresSID(t *testing.T) {
	got := executeSessionJSONError(t, []string{"session", "close"})
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "--sid is required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionKillRejectsUnsafeSignal(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		t.Fatalf("runSSH called for unsafe signal")
		return transport.Result{}
	}

	got := executeSessionJSONError(t, []string{
		"session", "kill", "--sid", "abcdef12", "--pid", "123",
		"--signal", "TERM; touch /tmp/pwned",
	})
	if got["error"] != "invalid_args" || got["message"] != "--signal must be a signal name or number" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestRemoteProcessListCommandQuotesFilterAndEscapesJSON(t *testing.T) {
	cmd := remoteProcessListCommand("foo'$(touch /tmp/pwned)", 5)
	if strings.Contains(cmd, "grep -i") {
		t.Fatalf("filter should not be interpolated into grep shell syntax: %s", cmd)
	}
	if !strings.Contains(cmd, `-v filter='foo'"'"'$(touch /tmp/pwned)'`) {
		t.Fatalf("filter was not shell-quoted as an awk variable: %s", cmd)
	}
	if !strings.Contains(cmd, "json_escape") {
		t.Fatalf("process list command does not escape JSON string fields: %s", cmd)
	}
}

func TestSessionCloseAcceptsJumpHost(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, command transport.SSHCommand, _ string) transport.Result {
		if command.Jump != "bastion.example.com" {
			t.Fatalf("command.Jump=%q want bastion.example.com", command.Jump)
		}
		return transport.Result{ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{"session", "close", "--sid", "abcdef12", "--jump", "bastion.example.com"})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionCloseTmuxMissingKeepsRegistry(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stdout:   nil,
			Stderr:   []byte("tmux_missing\n"),
			ExitCode: 127,
			Err:      &exec.ExitError{},
		}
	}

	got := executeJSONError(t, []string{"session", "close", "--sid", "abcdef12"})
	if got["error"] != "tmux_missing" {
		t.Fatalf("unexpected response: %#v", got)
	}

	if _, err := session.LoadRegistry(stateBaseDir(), "abcdef12"); err != nil {
		t.Fatalf("registry deleted despite remote close failure: %v", err)
	}
}
