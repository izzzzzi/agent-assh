package response

import (
	"encoding/json"
	"testing"
)

func TestOKJSON(t *testing.T) {
	body, err := Marshal(OK{"ok": true, "output_id": "abc123", "stdout_lines": 4})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got["ok"] != true || got["output_id"] != "abc123" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestErrorJSON(t *testing.T) {
	body, err := MarshalError("tmux_missing", "tmux is not installed", "retry with --install-tmux")
	if err != nil {
		t.Fatalf("MarshalError returned error: %v", err)
	}
	var got Error
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got.OK || got.Error != "tmux_missing" || got.Hint != "retry with --install-tmux" {
		t.Fatalf("unexpected error response: %#v", got)
	}
}
