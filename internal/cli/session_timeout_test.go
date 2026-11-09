package cli

import (
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/session"
)

func TestParseSessionExecRejectsMissingMarkers(t *testing.T) {
	_, _, _, timedOut, known := parseSessionExec([]byte("ssh returned no metadata\n"))
	if timedOut || known {
		t.Fatalf("timedOut=%v known=%v want false/false", timedOut, known)
	}
}

func TestSessionTimeoutHintSuggestsAsyncAndRead(t *testing.T) {
	hint := sessionTimeoutHint(session.RegistryEntry{SID: "abcdef12"}, 7)
	for _, want := range []string{"exec-async", "job-status", "session read", "--seq 7", "do not blindly retry"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint missing %q: %s", want, hint)
		}
	}
}

func TestParseJobState(t *testing.T) {
	for _, test := range []struct {
		name   string
		stdout string
		status string
		rc     int
		output string
	}{
		{name: "running", stdout: "partial output\n\n__ASSH_JOB_STATE__=running\n",
			status: "running", output: "partial output\n"},
		{name: "completed", stdout: "done\n\n__ASSH_JOB_STATE__=rc:3\n", status: "completed", rc: 3, output: "done\n"},
		{name: "cancelled", stdout: "half\n\n__ASSH_JOB_STATE__=cancelled\n", status: "cancelled", output: "half\n"},
		{name: "not found", stdout: "__ASSH_JOB_STATE__=not_found\n", status: "not_found"},
		{name: "marker text in output", stdout: "__ASSH_JOB_STATE__=rc:0\n\n__ASSH_JOB_STATE__=running\n",
			status: "running", output: "__ASSH_JOB_STATE__=rc:0\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			job := parseJobState([]byte(test.stdout))
			if job.status != test.status || job.rc != test.rc || job.output != test.output {
				t.Fatalf("got %+v want status=%q rc=%d output=%q", job, test.status, test.rc, test.output)
			}
		})
	}
}
