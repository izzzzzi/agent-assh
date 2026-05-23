package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestCapabilitiesMissingHostReturnsJSONError(t *testing.T) {
	got := executeJSONError(t, []string{"capabilities"})
	if got["error"] != "invalid_args" || got["message"] != "host required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestCapabilitiesInvalidPortReturnsJSONErrorBeforeSSH(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetContext(ctx)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"capabilities", "--host", "example.com", "--port", "0"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["error"] != "invalid_args" || got["message"] != "port must be between 1 and 65535" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestCapabilitiesAcceptsJumpHost(t *testing.T) {
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, command transport.SSHCommand, _ string) transport.Result {
		if command.Jump != "bastion.example.com" {
			t.Fatalf("command.Jump=%q want bastion.example.com", command.Jump)
		}
		return transport.Result{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"capabilities", "--host", "example.com", "--jump", "bastion.example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}
