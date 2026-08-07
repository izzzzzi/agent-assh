package cli

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/transport"
)

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

func TestSessionExecRejectsPipedStdin(t *testing.T) {
	// session exec forwards commands to tmux via non-interactive ssh and
	// never forwards its own stdin. A pipe like `cat f | assh session exec
	// ... ` used to silently drop the data and hang the tmux window while
	// the inner command (e.g. `docker exec -i`) waited on stdin forever.
	// Detect piped stdin early and return a typed error instead.
	writeTestSessionRegistry(t, "abcdef12")
	oldStdinPiped := stdinIsPiped
	t.Cleanup(func() { stdinIsPiped = oldStdinPiped })
	stdinIsPiped = func() bool { return true }

	args := []string{"session", "exec", "--sid", "abcdef12",
		"--", "docker", "exec", "-i", "c", "cat"}
	got := executeSessionJSONError(t, args)
	if got["ok"] != false || got["error"] != "stdin_pipe_unsupported" {
		t.Fatalf("expected stdin_pipe_unsupported, got %#v", got)
	}
	if hint, _ := got["hint"].(string); !strings.Contains(hint, "transfer put") {
		t.Fatalf("expected hint to mention transfer put, got %q", hint)
	}
}

func TestSessionExecAllowsNonPipedStdin(t *testing.T) {
	// Sanity check: when stdin is a TTY (normal interactive call), exec
	// must NOT be rejected by the piped-stdin guard.
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return sessionExecSuccessResult()
	}
	oldStdinPiped := stdinIsPiped
	t.Cleanup(func() { stdinIsPiped = oldStdinPiped })
	stdinIsPiped = func() bool { return false }

	got := executeSessionJSON(t, []string{"session", "exec", "--sid", "abcdef12", "--", "pwd"})
	if got["ok"] != true {
		t.Fatalf("expected ok, got %#v", got)
	}
}

func TestSessionExecBlocksDangerousCommandWithoutConfirmation(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		t.Fatalf("runSSH called for blocked dangerous command")
		return transport.Result{}
	}

	got := executeSessionJSONError(t, []string{"session", "exec", "--sid", "abcdef12", "--", "rm", "-rf", "/tmp/build"})
	if got["error"] != "dangerous_command_requires_confirmation" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["hint"] != "matched destructive pattern: rm_recursive" {
		t.Fatalf("unexpected hint: %#v", got)
	}

	entry, err := session.LoadRegistry(stateBaseDir(), "abcdef12")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if entry.Seq != 0 {
		t.Fatalf("blocked command incremented seq to %d", entry.Seq)
	}
}

func TestSessionExecConfirmDangerAllowsDangerousCommand(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	called := false
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, _ transport.SSHCommand, remoteCommand string) transport.Result {
		called = true
		if !strings.Contains(remoteCommand, "rm -rf /tmp/build") {
			t.Fatalf("remote command missing user command: %s", remoteCommand)
		}
		return sessionExecSuccessResult()
	}

	args := []string{"session", "exec", "--sid", "abcdef12",
		"--confirm-danger", "--", "rm", "-rf", "/tmp/build"}
	got := executeSessionJSON(t, args)
	if got["ok"] != true || got["seq"] != float64(1) {
		t.Fatalf("unexpected response: %#v", got)
	}
	if !called {
		t.Fatalf("runSSH was not called")
	}
}

func TestSessionExecDoesNotBlockQuotedDangerousText(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{
			Stdout:   []byte("__ASSH_RC__=0\n__ASSH_STDOUT_LINES__=1\n__ASSH_STDERR_LINES__=0\n"),
			ExitCode: 0,
		}
	}

	got := executeSessionJSON(t, []string{"session", "exec", "--sid", "abcdef12", "--", "echo", `"rm -rf /"`})
	if got["ok"] != true {
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

func TestSessionExecAcceptsJumpHost(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, command transport.SSHCommand, _ string) transport.Result {
		if command.Jump != "bastion.example.com" {
			t.Fatalf("command.Jump=%q want bastion.example.com", command.Jump)
		}
		return sessionExecSuccessResult()
	}

	args := []string{"session", "exec", "--sid", "abcdef12",
		"--jump", "bastion.example.com", "--", "pwd"}
	got := executeSessionJSON(t, args)
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionExecUsesStoredJumpHost(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	entry, err := session.LoadRegistry(stateBaseDir(), "abcdef12")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	entry.Jump = "bastion.example.com"
	if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, command transport.SSHCommand, _ string) transport.Result {
		if command.Jump != "bastion.example.com" {
			t.Fatalf("command.Jump=%q want stored jump host", command.Jump)
		}
		return sessionExecSuccessResult()
	}

	got := executeSessionJSON(t, []string{"session", "exec", "--sid", "abcdef12", "--", "pwd"})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionExecNonZeroRCIsCommandResult(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stdout:   []byte("__ASSH_RC__=2\n__ASSH_STDOUT_LINES__=3\n__ASSH_STDERR_LINES__=1\n"),
			ExitCode: 0,
		}
	}

	args := []string{"session", "exec", "--sid", "abcdef12", "--", "false"}
	got := executeSessionJSON(t, args)
	if got["ok"] != true || got["rc"] != float64(2) {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["stdout_lines"] != float64(3) || got["stderr_lines"] != float64(1) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionExecTimeoutMarkerReturnsTimeout(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stdout:   []byte("__ASSH_TIMEOUT__\n"),
			Stderr:   []byte("timeout"),
			ExitCode: 124,
			Err:      &exec.ExitError{},
		}
	}

	args := []string{"session", "exec", "--sid", "abcdef12",
		"--timeout", "1", "--", "sleep", "10"}
	got := executeSessionJSONError(t, args)
	if got["ok"] != false || got["error"] != "timeout" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionExecUsesConfiguredTimeout(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatalf("context deadline is not set")
		}
		remaining := time.Until(deadline)
		if remaining <= 10*time.Second || remaining > 13*time.Second {
			t.Fatalf("context timeout = %v, want about 12s", remaining)
		}
		if command.TimeoutSecond != 12 {
			t.Fatalf("ssh timeout = %d, want 12", command.TimeoutSecond)
		}
		if !strings.Contains(remoteCommand, "while [ $i -lt 7 ]") {
			t.Fatalf("remote command does not use configured wait: %s", remoteCommand)
		}
		return transport.Result{
			Stdout:   sessionExecSuccessResult().Stdout,
			ExitCode: 0,
		}
	}

	got := executeSessionJSON(t, []string{"session", "exec", "--sid", "abcdef12", "--timeout", "7", "--", "pwd"})
	if got["ok"] != true || got["sid"] != "abcdef12" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionJobStatusRejectsInvalidJobID(t *testing.T) {
	// --job-id flows into a remote `mkdir -p .../<jobName>` and tmux
	// window name with NO shell quoting. Without ids.Valid() enforcement
	// a caller-supplied "x; touch /tmp/pwned; #" executes arbitrary
	// commands on the remote host. Reject anything that is not a valid
	// assh id (hex, 8-32 chars) up front.
	writeTestSessionRegistry(t, "abcdef12")

	for _, evil := range []string{
		"x; touch /tmp/pwned",
		"../etc",
		"with space",
		"sh -c bad",
		"AA12cc", // uppercase not allowed (hex only)
		"abc",    // too short
	} {
		got := executeSessionJSONError(t, []string{"session", "job-status", "--sid", "abcdef12", "--job-id", evil})
		if got["ok"] != false || got["error"] != "invalid_args" {
			t.Fatalf("job-id=%q expected invalid_args, got %#v", evil, got)
		}
	}
}

func TestSessionJobCancelRejectsInvalidJobID(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")

	got := executeSessionJSONError(t, []string{"session", "job-cancel", "--sid", "abcdef12", "--job-id", "x; rm -rf /"})
	if got["ok"] != false || got["error"] != "invalid_args" {
		t.Fatalf("expected invalid_args, got %#v", got)
	}
}

func TestSessionJobStatusAcceptsValidJobID(t *testing.T) {
	// Sanity: a real assh job id (hex) must still be accepted.
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		if !strings.Contains(remoteCommand, "assh_job_deadbeef12345678") {
			t.Fatalf("remoteCommand=%q missing valid job name", remoteCommand)
		}
		// The job name must appear ONLY as a path/window-name suffix, never
		// as a shell fragment. Assert the literal injected payload markers
		// are absent right after the job name.
		for _, forbidden := range []string{"; ", " && ", " || "} {
			if strings.Contains(remoteCommand, "assh_job_deadbeef12345678"+forbidden) {
				t.Fatalf("remoteCommand=%q job name leaked as shell fragment", remoteCommand)
			}
		}
		return transport.Result{Stdout: []byte("__JOB_NOT_FOUND__\n"), ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{"session", "job-status", "--sid", "abcdef12", "--job-id", "deadbeef12345678"})
	if got["ok"] != true {
		t.Fatalf("expected ok with valid job-id, got %#v", got)
	}
}
