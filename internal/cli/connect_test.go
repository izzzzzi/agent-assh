package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/agent-ssh/assh/internal/bootstrap"
)

func TestRootIncludesConnectCommand(t *testing.T) {
	cmd := NewRootCommand()
	found, _, err := cmd.Find([]string{"connect"})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if found == nil || found.Name() != "connect" {
		t.Fatalf("Find(connect) = %v, want connect command", found)
	}
}

func TestConnectRequiresHost(t *testing.T) {
	var stderr bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"connect"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}

	var got map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("expected json stderr, got %q", stderr.String())
	}
	if got["ok"] != false || got["error"] != "invalid_args" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestConnectMapsBootstrapErrorToJSON(t *testing.T) {
	original := newBootstrapService
	t.Cleanup(func() { newBootstrapService = original })
	newBootstrapService = func() bootstrap.Service {
		return bootstrap.Service{
			EnsureKeyPair: func(string) error { return nil },
			RunSSH: func(context.Context, bootstrap.SSHTarget, string) bootstrap.SSHResult {
				return bootstrap.SSHResult{
					Stderr:   []byte("Permission denied"),
					ExitCode: 255,
					Err:      errors.New("ssh failed"),
				}
			},
			NewID: func() (string, error) { return "abc12345", nil },
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"connect", "-H", "10.0.0.1"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q want empty", stdout.String())
	}
	var got map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("expected json stderr, got %q", stderr.String())
	}
	if got["ok"] != false || got["error"] != "auth_failed" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if strings.Contains(stderr.String(), "secret") {
		t.Fatalf("stderr leaked password-like data: %q", stderr.String())
	}
}
