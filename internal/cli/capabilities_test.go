package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
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
