package cli

import (
	"strings"
	"testing"
)

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
