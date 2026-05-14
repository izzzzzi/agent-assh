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

func TestReadMissingAndEmptyFilesReturnEmptySlice(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{
			name: "missing",
		},
		{
			name: "empty",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "audit.jsonl")
			if test.setup != nil {
				test.setup(t, path)
			}

			got, err := Read(path, Filter{})
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if got == nil {
				t.Fatalf("Read() returned nil slice")
			}
			if len(got) != 0 {
				t.Fatalf("Read() returned %d events, want 0: %#v", len(got), got)
			}
		})
	}
}

func TestReadMalformedJSONLReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(path, []byte("{bad json}\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := Read(path, Filter{}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestReadAppliesLastAfterFilters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	events := []Event{
		{Action: "exec", Host: "a.example", ExitCode: 1},
		{Action: "exec", Host: "b.example", ExitCode: 1},
		{Action: "exec", Host: "a.example", ExitCode: 2},
		{Action: "exec", Host: "a.example", ExitCode: 3},
	}
	for _, event := range events {
		if err := Write(path, event); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	got, err := Read(path, Filter{Last: 2, Host: "a.example", Failed: true})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got) != 2 || got[0].ExitCode != 2 || got[1].ExitCode != 3 {
		t.Fatalf("unexpected events: %#v", got)
	}
}

func TestReadFailedExcludesZeroExitCodeAndIncludesNonZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	events := []Event{
		{Action: "exec", Host: "a.example"},
		{Action: "exec", Host: "a.example", ExitCode: 0},
		{Action: "exec", Host: "a.example", ExitCode: 1},
	}
	for _, event := range events {
		if err := Write(path, event); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	got, err := Read(path, Filter{Failed: true})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got) != 1 || got[0].ExitCode != 1 {
		t.Fatalf("unexpected events: %#v", got)
	}
}
