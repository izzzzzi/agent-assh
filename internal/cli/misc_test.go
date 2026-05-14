package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-ssh/assh/internal/audit"
)

func TestAuditCommandFiltersHostAndFailed(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	path := filepath.Join(stateBaseDir(), "audit", "audit.jsonl")
	events := []audit.Event{
		{Action: "exec", Host: "a.example", ExitCode: 0},
		{Action: "exec", Host: "a.example", ExitCode: 2},
		{Action: "exec", Host: "b.example", ExitCode: 1},
	}
	for _, event := range events {
		if err := audit.Write(path, event); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"audit", "--last", "10", "--host", "a.example", "--failed"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var got []audit.Event
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if len(got) != 1 || got[0].Host != "a.example" || got[0].ExitCode != 2 {
		t.Fatalf("unexpected events: %#v", got)
	}
}

func TestAuditCommandMissingAndEmptyFilesOutputEmptyArray(t *testing.T) {
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
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ASSH_STATE_DIR", t.TempDir())
			path := filepath.Join(stateBaseDir(), "audit", "audit.jsonl")
			if test.setup != nil {
				test.setup(t, path)
			}

			var out bytes.Buffer
			cmd := NewRootCommand()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"audit"})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got := out.String(); got != "[]\n" {
				t.Fatalf("output = %q, want %q", got, "[]\n")
			}
		})
	}
}

func TestAuditCommandAllFilteredOutputsEmptyArray(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	path := filepath.Join(stateBaseDir(), "audit", "audit.jsonl")
	if err := audit.Write(path, audit.Event{Action: "exec", Host: "a.example", ExitCode: 0}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"audit", "--host", "b.example", "--failed"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); got != "[]\n" {
		t.Fatalf("output = %q, want %q", got, "[]\n")
	}
}

func TestAuditCommandDropsLegacyCommandField(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	path := filepath.Join(stateBaseDir(), "audit", "audit.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"action":"exec","host":"a.example","exit_code":1,"command":"secret"}`+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"audit"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Contains(out.String(), "command") || strings.Contains(out.String(), "secret") {
		t.Fatalf("legacy command leaked in output: %q", out.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if len(got) != 1 || got[0]["host"] != "a.example" {
		t.Fatalf("unexpected events: %#v", got)
	}
}
