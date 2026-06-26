package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/transport"
)

// runExecJSON runs `exec` with a stubbed SSH and returns the parsed JSON.
func runExecJSON(t *testing.T, stdout string, args []string) map[string]any {
	t.Helper()
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, _ transport.SSHCommand, _ string) transport.Result {
		return transport.Result{ExitCode: 0, Stdout: []byte(stdout)}
	}
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v out=%q", err, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %q", out.String())
	}
	return got
}

func runCmdJSON(t *testing.T, args []string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v out=%q", err, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %q", out.String())
	}
	return got
}

func TestExecRedactsStoredOutput(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	got := runExecJSON(t, "user=bob\npassword=topsecret123\n",
		[]string{"exec", "--host", "h", "--user", "root", "--", "cat", "conf"})
	if got["redacted"] != true {
		t.Errorf("expected redacted=true, got %#v", got["redacted"])
	}
	if c, ok := got["redaction_count"].(float64); !ok || c < 1 {
		t.Errorf("expected redaction_count>=1, got %#v", got["redaction_count"])
	}
}

func TestExecNoRedactDisables(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	got := runExecJSON(t, "password=topsecret123\n",
		[]string{"exec", "--host", "h", "--user", "root", "--no-redact", "--", "cat", "conf"})
	if got["redacted"] != false {
		t.Errorf("expected redacted=false with --no-redact, got %#v", got["redacted"])
	}
}

func TestAuditSavingsAggregates(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	// Produce output (10 lines), then read a 3-line window so 7 are withheld.
	exec := runExecJSON(t, "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n",
		[]string{"exec", "--host", "h", "--user", "root", "--", "cat", "f"})
	id, _ := exec["output_id"].(string)
	if id == "" {
		t.Fatal("no output_id")
	}
	_ = runCmdJSON(t, []string{"read", "--id", id, "--limit", "3"})

	got := runCmdJSON(t, []string{"audit", "--savings"})
	if got["ok"] != true {
		t.Fatalf("savings not ok: %#v", got)
	}
	if got["raw_lines"] != float64(10) {
		t.Errorf("raw_lines = %#v, want 10", got["raw_lines"])
	}
	if got["served_lines"] != float64(3) {
		t.Errorf("served_lines = %#v, want 3", got["served_lines"])
	}
	if got["withheld_lines"] != float64(7) {
		t.Errorf("withheld_lines = %#v, want 7", got["withheld_lines"])
	}
}

func TestAuditSavingsBypassesLastGuard(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	// No reads yet; --savings must still succeed (not trip --last>=1).
	got := runCmdJSON(t, []string{"audit", "--savings"})
	if got["ok"] != true {
		t.Fatalf("expected ok summary, got %#v", got)
	}
	if got["reads"] != float64(0) {
		t.Errorf("reads = %#v, want 0", got["reads"])
	}
}
