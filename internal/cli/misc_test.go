package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
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
