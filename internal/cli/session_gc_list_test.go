package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestSessionGCReturnsDryRunCandidates(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	got := executeSessionJSON(t, []string{"session", "gc"})

	candidates, ok := got["candidates"].([]any)
	if !ok {
		t.Fatalf("candidates = %#v, want JSON array", got["candidates"])
	}
	if got["ok"] != true || got["dry_run"] != true || len(candidates) != 0 {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionListReturnsEmptyRegistryJSON(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())

	got := executeSessionJSON(t, []string{"session", "list"})

	if got["ok"] != true || got["count"] != float64(0) {
		t.Fatalf("unexpected response: %#v", got)
	}
	sessions, ok := got["sessions"].([]any)
	if !ok || len(sessions) != 0 {
		t.Fatalf("sessions = %#v", got["sessions"])
	}
}

func TestSessionListReturnsSortedSessions(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	now := time.Now().UTC()
	entries := []session.RegistryEntry{
		{
			SID: "aaaaaaaa", Label: "tie-a", Host: "a.example", User: "root", Port: 22,
			HostKeyPolicy: "accept-new", TmuxName: "assh_aaaaaaaa",
			CreatedAt: now.Add(-2 * time.Second), TTLSeconds: 3600, Seq: 1,
		},
		{
			SID: "cccccccc", Label: "tie-c", Host: "c.example", User: "root", Port: 22,
			HostKeyPolicy: "accept-new", TmuxName: "assh_cccccccc",
			CreatedAt: now.Add(-2 * time.Second), TTLSeconds: 1, Seq: 3,
		},
		{
			SID: "bbbbbbbb", Label: "older", Host: "b.example", User: "root", Port: 22,
			HostKeyPolicy: "accept-new", TmuxName: "assh_bbbbbbbb",
			CreatedAt: now.Add(-time.Hour), TTLSeconds: 7200, Seq: 2,
		},
	}
	for _, entry := range entries {
		if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
			t.Fatalf("SaveRegistry() error = %v", err)
		}
	}

	got := executeSessionJSON(t, []string{"session", "list"})

	if got["ok"] != true || got["count"] != float64(3) {
		t.Fatalf("unexpected response: %#v", got)
	}
	sessions := got["sessions"].([]any)
	if len(sessions) != 3 {
		t.Fatalf("sessions = %#v", got["sessions"])
	}

	first := sessions[0].(map[string]any)
	second := sessions[1].(map[string]any)
	third := sessions[2].(map[string]any)

	if first["sid"] != "aaaaaaaa" || second["sid"] != "cccccccc" || third["sid"] != "bbbbbbbb" {
		t.Fatalf("unexpected order: %#v", got["sessions"])
	}
	if first["expired"] != false || second["expired"] != true || third["expired"] != false {
		t.Fatalf("unexpected expired flags: %#v", got["sessions"])
	}
	if first["seq"] != float64(1) || first["tmux_name"] != "assh_aaaaaaaa" {
		t.Fatalf("unexpected session payload: %#v", first)
	}
}

func TestSessionGCDryRunFiltersByHostAndAge(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	now := time.Now().UTC()
	entries := []session.RegistryEntry{
		{
			SID: "abcdef12", Host: "a.example", User: "root", Port: 22,
			HostKeyPolicy: "accept-new", TmuxName: "assh_abcdef12",
			CreatedAt: now.Add(-48 * time.Hour), TTLSeconds: 3600,
		},
		{
			SID: "abcdef13", Host: "b.example", User: "root", Port: 22,
			HostKeyPolicy: "accept-new", TmuxName: "assh_abcdef13",
			CreatedAt: now.Add(-48 * time.Hour), TTLSeconds: 3600,
		},
	}
	for _, entry := range entries {
		if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
			t.Fatalf("SaveRegistry() error = %v", err)
		}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"session", "gc", "--host", "a.example", "--older-than", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got["dry_run"] != true {
		t.Fatalf("expected dry run: %#v", got)
	}
	candidates := got["candidates"].([]any)
	if len(candidates) != 1 || candidates[0] != "abcdef12" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
}

func TestSessionGCDryRunDoesNotRunSSHOrDeleteRegistry(t *testing.T) {
	writeExpiredSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		t.Fatalf("runSSH called during dry run")
		return transport.Result{}
	}

	got := executeSessionJSON(t, []string{"session", "gc"})
	if got["dry_run"] != true {
		t.Fatalf("expected dry run: %#v", got)
	}
	if _, err := session.LoadRegistry(stateBaseDir(), "abcdef12"); err != nil {
		t.Fatalf("registry missing after dry run: %v", err)
	}
}

func TestSessionGCExecuteRemoteFailureKeepsRegistryAndReportsError(t *testing.T) {
	writeExpiredSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stderr:   []byte("metadata validation failed"),
			ExitCode: 3,
			Err:      &exec.ExitError{},
		}
	}

	got := executeSessionJSON(t, []string{"session", "gc", "--execute"})
	errors := got["errors"].([]any)
	if len(errors) != 1 {
		t.Fatalf("expected one cleanup error: %#v", got)
	}
	cleanupError := errors[0].(map[string]any)
	if cleanupError["sid"] != "abcdef12" || cleanupError["error"] == "" {
		t.Fatalf("unexpected cleanup error: %#v", cleanupError)
	}
	if _, err := session.LoadRegistry(stateBaseDir(), "abcdef12"); err != nil {
		t.Fatalf("registry deleted after failed remote cleanup: %v", err)
	}
}

func TestSessionGCExecuteSuccessDeletesRegistryAndReportsDeleted(t *testing.T) {
	writeExpiredSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{"session", "gc", "--execute"})
	deleted := got["deleted"].([]any)
	if len(deleted) != 1 || deleted[0] != "abcdef12" {
		t.Fatalf("unexpected deleted list: %#v", got)
	}
	if _, err := session.LoadRegistry(stateBaseDir(), "abcdef12"); err == nil {
		t.Fatalf("registry still present after successful remote cleanup")
	}
}
