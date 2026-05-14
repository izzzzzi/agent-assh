package cli

import (
	"bytes"
	"encoding/json"
	"testing"
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
