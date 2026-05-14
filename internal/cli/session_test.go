package cli

import (
	"bytes"
	"encoding/json"
	"testing"
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
	got := executeSessionJSON(t, []string{"session", "open", "--host", "example.com", "--name", "deploy", "--install-tmux"})

	if got["ok"] != true || got["operation"] != "session_open" || got["host"] != "example.com" || got["label"] != "deploy" || got["install_tmux"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionCloseRequiresSID(t *testing.T) {
	got := executeSessionJSONError(t, []string{"session", "close"})
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "--sid is required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

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

func TestSessionExecReturnsJSON(t *testing.T) {
	got := executeSessionJSON(t, []string{"session", "exec", "--sid", "abcdef12", "--", "pwd"})
	if got["ok"] != true || got["operation"] != "session_exec" || got["sid"] != "abcdef12" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

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
	got := executeSessionJSON(t, []string{"session", "read", "--sid", "abcdef12", "--seq", "2"})
	if got["ok"] != true || got["operation"] != "session_read" || got["sid"] != "abcdef12" || got["seq"] != float64(2) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionGCReturnsDryRunCandidates(t *testing.T) {
	got := executeSessionJSON(t, []string{"session", "gc"})

	candidates, ok := got["candidates"].([]any)
	if !ok {
		t.Fatalf("candidates = %#v, want JSON array", got["candidates"])
	}
	if got["ok"] != true || got["dry_run"] != true || len(candidates) != 0 {
		t.Fatalf("unexpected response: %#v", got)
	}
}

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
