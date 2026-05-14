package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestUnknownCommandReturnsJSONError(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"unknown"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	var got map[string]any
	if json.Unmarshal(out.Bytes(), &got) != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != false || got["error"] != "invalid_args" || got["hint"] != "run assh --help" {
		t.Fatalf("unexpected response: %#v", got)
	}
}
