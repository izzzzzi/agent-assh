package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestDBQueryReportsRemoteFailure(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{
			Stdout: []byte("ERROR 2002 (HY000): Can't connect to local MySQL server through socket " +
				"'/var/run/mysqld/mysqld.sock' (2)"),
			ExitCode: 1,
		}
	}

	got := executeSessionJSONError(t, []string{
		"session", "db-query", "--sid", "abcdef12",
		"--database", "likeboom", "--query", "SELECT 1",
	})
	if got["error"] != "command_failed" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if !strings.Contains(got["message"].(string), "Can't connect") {
		t.Fatalf("message should carry the db error output: %#v", got)
	}
}

func TestDBQuerySuccessIncludesExitCode(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{Stdout: []byte("42\n"), ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{
		"session", "db-query", "--sid", "abcdef12",
		"--database", "likeboom", "--query", "SELECT 42",
	})
	if got["ok"] != true || got["exit_code"] != float64(0) || got["output"] != "42\n" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestDockerExecReportsRemoteFailure(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{
			Stdout:   []byte("ERROR 1045 (28000): Access denied for user 'root'"),
			ExitCode: 1,
		}
	}

	got := executeSessionJSONError(t, []string{
		"session", "docker-exec", "--sid", "abcdef12",
		"--container", "likeboom-mysql-1", "--", "mysql", "-e", "SELECT 1",
	})
	if got["error"] != "command_failed" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestDockerPSReportsRemoteFailure(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{
			Stdout:   []byte("Cannot connect to the Docker daemon"),
			ExitCode: 1,
		}
	}

	got := executeSessionJSONError(t, []string{
		"session", "docker-ps", "--sid", "abcdef12",
	})
	if got["error"] != "command_failed" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestExecAsyncJobRunsUnderBashFirst(t *testing.T) {
	cmd := execAsyncRemoteCommand("assh_abcdef12", "job_abcdef12_1", 1,
		"source /root/.bashrc && echo hi", 300)
	if !strings.Contains(cmd, "if command -v bash >/dev/null 2>&1; then bash") {
		t.Fatalf("missing bash-first runner: %s", cmd)
	}
	if strings.Contains(cmd, "sh -c 'sh ") {
		t.Fatalf("bare sh runner still present: %s", cmd)
	}
	if !strings.Contains(cmd, "__JOB_COMPLETE__") {
		t.Fatalf("job marker missing: %s", cmd)
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

func TestExecDestructiveHintPointsToSessionExec(t *testing.T) {
	got := executeTransferJSONError(t, []string{
		"exec", "--host", "example.com", "--", "rm", "-rf", "/tmp/x",
	})
	if got["error"] != "dangerous_command_requires_confirmation" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if !strings.Contains(got["message"].(string), "session exec") {
		t.Fatalf("hint should point to session exec: %#v", got)
	}
}
