package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/forward"
	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestForwardStartWritesStateAndReturnsControlSocket(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	oldRunForward := runForward
	t.Cleanup(func() { runForward = oldRunForward })
	runForward = func(_ context.Context, command transport.SSHCommand, args []string) transport.Result {
		if command.Host != "example.com" || command.User != "root" || command.Jump != "bastion.example.com" {
			t.Fatalf("command=%#v", command)
		}
		for _, want := range []string{"-N", "-f", "-M", "-L", "127.0.0.1:8080:127.0.0.1:80", "-D", "127.0.0.1:1080"} {
			if !containsString(args, want) {
				t.Fatalf("args=%#v missing %q", args, want)
			}
		}
		return transport.Result{ExitCode: 0}
	}

	got := executeForwardJSON(t, []string{
		"forward", "start",
		"--name", "deploy",
		"--host", "example.com",
		"--jump", "bastion.example.com",
		"--local-forward", "127.0.0.1:8080:127.0.0.1:80",
		"--dynamic-forward", "127.0.0.1:1080",
	})

	if got["ok"] != true || got["name"] != "deploy" || got["socket"] == "" {
		t.Fatalf("unexpected response: %#v", got)
	}
	rules := got["rules"].(map[string]any)
	if len(rules["local"].([]any)) != 1 || len(rules["dynamic"].([]any)) != 1 {
		t.Fatalf("unexpected rules: %#v", got)
	}

	record, err := state.NewForwardStore(stateBaseDir()).Load("deploy")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if record.Jump != "bastion.example.com" || len(record.Local) != 1 || len(record.Dynamic) != 1 || record.ControlSocket == "" {
		t.Fatalf("record=%#v", record)
	}
}

func TestForwardStatusReadsSavedState(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	store := state.NewForwardStore(stateBaseDir())
	record := testForwardRecord()
	if err := store.Save(record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	oldRunForward := runForward
	t.Cleanup(func() { runForward = oldRunForward })
	runForward = func(_ context.Context, command transport.SSHCommand, args []string) transport.Result {
		if command.Host != record.Host || !containsString(args, "check") {
			t.Fatalf("command=%#v args=%#v", command, args)
		}
		return transport.Result{ExitCode: 0}
	}

	got := executeForwardJSON(t, []string{"forward", "status", "--name", "deploy"})

	if got["ok"] != true || got["name"] != "deploy" || got["live"] != true || got["socket"] != record.ControlSocket {
		t.Fatalf("unexpected response: %#v", got)
	}
	rules := got["rules"].(map[string]any)
	if len(rules["local"].([]any)) != 1 || len(rules["remote"].([]any)) != 1 {
		t.Fatalf("unexpected rules: %#v", got)
	}
}

func TestForwardStatusReportsNotLiveOnCheckFailure(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	store := state.NewForwardStore(stateBaseDir())
	if err := store.Save(testForwardRecord()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	oldRunForward := runForward
	t.Cleanup(func() { runForward = oldRunForward })
	runForward = func(context.Context, transport.SSHCommand, []string) transport.Result {
		return transport.Result{ExitCode: 255, Err: errors.New("not running")}
	}

	got := executeForwardJSON(t, []string{"forward", "status", "--name", "deploy"})
	if got["ok"] != true || got["live"] != false {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestForwardStopRemovesStateEntry(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	store := state.NewForwardStore(stateBaseDir())
	record := testForwardRecord()
	if err := store.Save(record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	oldRunForward := runForward
	t.Cleanup(func() { runForward = oldRunForward })
	runForward = func(_ context.Context, command transport.SSHCommand, args []string) transport.Result {
		if command.Host != record.Host || !containsString(args, "exit") {
			t.Fatalf("command=%#v args=%#v", command, args)
		}
		return transport.Result{ExitCode: 0}
	}

	got := executeForwardJSON(t, []string{"forward", "stop", "--name", "deploy"})

	if got["ok"] != true || got["stopped"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
	if _, err := store.Load("deploy"); err == nil {
		t.Fatal("forward record still exists after stop")
	}
}

func TestForwardValidatesRequiredArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "subcommand", args: []string{"forward"}, want: "forward subcommand required"},
		{name: "start name", args: []string{"forward", "start", "--host", "example.com", "--local-forward", "8080:localhost:80"}, want: "--name is required"},
		{name: "start rule", args: []string{"forward", "start", "--name", "deploy", "--host", "example.com"}, want: "at least one forwarding rule required"},
		{name: "status name", args: []string{"forward", "status"}, want: "--name is required"},
		{name: "stop name", args: []string{"forward", "stop"}, want: "--name is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := executeForwardJSONError(t, test.args)
			if got["error"] != "invalid_args" || got["message"] != test.want {
				t.Fatalf("unexpected response: %#v", got)
			}
		})
	}
}

func TestPromptManifestIncludesForwardCommand(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "assh forward status --name NAME") {
		t.Fatalf("prompt manifest missing forward command in %s", out.String())
	}
}

func testForwardRecord() state.ForwardRecord {
	return state.ForwardRecord{
		Name:             "deploy",
		Host:             "example.com",
		User:             "root",
		Port:             22,
		Jump:             "bastion.example.com",
		HostKeyPolicy:    "accept-new",
		Local:            []string{"127.0.0.1:8080:127.0.0.1:80"},
		Remote:           []string{"9000:127.0.0.1:9000"},
		ControlSocket:    forward.ControlSocketPath(stateBaseDir(), "deploy"),
		PersistSeconds:   3600,
		TimeoutSeconds:   300,
		CreatedAtRFC3339: "2026-05-23T00:00:00Z",
	}
}

func executeForwardJSON(t *testing.T, args []string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, output=%q", err, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	return got
}

func executeForwardJSONError(t *testing.T, args []string) map[string]any {
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want || strings.Contains(value, want) {
			return true
		}
	}
	return false
}
