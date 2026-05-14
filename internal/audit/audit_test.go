package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOmitsCommandText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	if err := Write(path, Event{
		Action:      "exec",
		Host:        "h",
		User:        "root",
		CommandHash: "abc",
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if _, ok := entry["command"]; ok {
		t.Fatalf("audit entry contains command key: %s", data)
	}
}
