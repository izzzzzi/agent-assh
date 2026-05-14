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

func TestReadFiltersAuditEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	events := []Event{
		{Action: "exec", Host: "a.example", ExitCode: 0},
		{Action: "exec", Host: "a.example", ExitCode: 2},
		{Action: "exec", Host: "b.example", ExitCode: 1},
	}
	for _, event := range events {
		if err := Write(path, event); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	got, err := Read(path, Filter{Last: 10, Host: "a.example", Failed: true})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got) != 1 || got[0].Host != "a.example" || got[0].ExitCode != 2 {
		t.Fatalf("unexpected events: %#v", got)
	}
}
