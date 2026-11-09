package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestExecAsyncJobRunsUnderBashFirst(t *testing.T) {
	cmd := execAsyncRemoteCommand("abcdef12", "assh_abcdef12", "job_abcdef12_1",
		"source /root/.bashrc && echo hi")
	if !strings.Contains(cmd, "if command -v bash >/dev/null 2>&1; then bash") {
		t.Fatalf("missing bash-first runner: %s", cmd)
	}
	if strings.Contains(cmd, "sh -c 'sh ") {
		t.Fatalf("bare sh runner still present: %s", cmd)
	}
	if !strings.Contains(cmd, "~/.assh/sessions/abcdef12/jobs/job_abcdef12_1/rc") {
		t.Fatalf("job files must live inside the session directory: %s", cmd)
	}
}

func TestExecSyncPTYRunnerUsesBashFirst(t *testing.T) {
	cmd := execSyncRemoteCommandPTY("abcdef12", "assh_abcdef12", 1,
		"source /root/.profile && ls", 30)
	if !strings.Contains(cmd, "if command -v bash >/dev/null 2>&1; then bash") {
		t.Fatalf("missing bash-first runner: %s", cmd)
	}
	if strings.Contains(cmd, "-n exec sh -c 'sh ") {
		t.Fatalf("bare sh runner still present: %s", cmd)
	}
}

// TestExecDestructiveHintPointsToSessionExec pins the block message to the real
// behaviour. `assh exec` has no --confirm-danger by design, so the message must
// say so and the hint must point at the session path that does support it.
func TestExecDestructiveHintPointsToSessionExec(t *testing.T) {
	got := executeTransferJSONError(t, []string{
		"exec", "--host", "example.com", "--", "rm", "-rf", "/tmp/x",
	})
	if got["error"] != "dangerous_command_requires_confirmation" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if message, _ := got["message"].(string); !strings.Contains(message, "does not support --confirm-danger") {
		t.Fatalf("message should state the flag is unsupported: %#v", got)
	}
	if hint, _ := got["hint"].(string); !strings.Contains(hint, "session exec") {
		t.Fatalf("hint should point to session exec: %#v", got)
	}
}

func TestExecBlockedCommandNeverCallsRunSSH(t *testing.T) {
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		t.Fatalf("runSSH called for a blocked dangerous command")
		return transport.Result{}
	}

	got := executeTransferJSONError(t, []string{
		"exec", "--host", "example.com", "--", "rm", "-rf", "/tmp/x",
	})
	if got["error"] != "dangerous_command_requires_confirmation" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

// The PTY runner must print every marker parseSessionExec requires; without the
// stderr one each exec on a --force-pty session ended as command_status_unknown.
func TestExecSyncPTYRunnerPrintsAllMarkers(t *testing.T) {
	cmd := execSyncRemoteCommandPTY("abcdef12", "assh_abcdef12", 3, "ls", 30)
	for _, marker := range []string{"__ASSH_RC__=", "__ASSH_STDOUT_LINES__=", "__ASSH_STDERR_LINES__="} {
		if !strings.Contains(cmd, marker) {
			t.Fatalf("PTY runner misses %s: %s", marker, cmd)
		}
	}
	if !strings.Contains(cmd, "~/.assh/sessions/abcdef12/jobs/exec_3/") {
		t.Fatalf("PTY job files must live inside the session directory: %s", cmd)
	}
}

func TestTransferMarkersBecomeTypedErrors(t *testing.T) {
	for _, test := range []struct {
		stdout string
		parsed int
		want   string
	}{
		{"__ASSH_NOT_FOUND__\n", 0, "remote_file_not_found"},
		{"__ASSH_UNSUPPORTED__\n", 0, "unsupported_remote"},
		{"{\"name\":\"a\"}\n__ASSH_UNSUPPORTED__\n", 1, ""},
		{"{\"name\":\"a\"}\n", 1, ""},
	} {
		cmd := NewRootCommand()
		var stderr strings.Builder
		cmd.SetErr(&stderr)
		err := transferMarkerError(cmd, []byte(test.stdout), "/x", test.parsed)
		if (err != nil) != (test.want != "") || !strings.Contains(stderr.String(), test.want) {
			t.Fatalf("stdout=%q: err=%v stderr=%q want %q", test.stdout, err, stderr.String(), test.want)
		}
	}
}

func TestTransferSyncRejectsRemoteShellMetacharacters(t *testing.T) {
	got := executeTransferJSONError(t, []string{
		"transfer", "sync", "--host", "example.com", "--source", "./dist", "--dest", "/srv/$(id)",
	})
	if got["error"] != "invalid_args" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestTransferRmRefusesSystemPath(t *testing.T) {
	got := executeTransferJSONError(t, []string{
		"transfer", "rm", "--host", "example.com", "--path", "/etc", "--recursive",
	})
	if got["error"] != "dangerous_command_requires_confirmation" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestTailBytesCutsAtLineBoundary(t *testing.T) {
	if got, cut := tailBytes([]byte("short\n"), 100); got != "short\n" || cut {
		t.Fatalf("got %q cut=%v", got, cut)
	}
	if got, cut := tailBytes([]byte("aaaa\nbbbb\ncccc\n"), 8); got != "cccc\n" || !cut {
		t.Fatalf("got %q cut=%v", got, cut)
	}
}

func TestParseSessionReadHandlesMarkerWithoutLeadingNewline(t *testing.T) {
	content, total, notFound := parseSessionRead([]byte("__ASSH_TOTAL_LINES__=0"))
	if content != "" || total != 0 || notFound {
		t.Fatalf("content=%q total=%d notFound=%v", content, total, notFound)
	}
	content, total, _ = parseSessionRead([]byte("a\nb\n\n__ASSH_TOTAL_LINES__=7\n"))
	if content != "a\nb\n" || total != 7 {
		t.Fatalf("content=%q total=%d", content, total)
	}
}
