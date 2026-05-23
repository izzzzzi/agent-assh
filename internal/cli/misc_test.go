package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/audit"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/transport"
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

func TestScanAcceptsJumpHost(t *testing.T) {
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, command transport.SSHCommand, _ string) transport.Result {
		if command.Jump != "bastion.example.com" {
			t.Fatalf("command.Jump=%q want bastion.example.com", command.Jump)
		}
		return transport.Result{ExitCode: 0, Stdout: []byte("{}\n")}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"scan", "--host", "example.com", "--jump", "bastion.example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestKeyDeployUsesJumpHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake ssh is unix-only")
	}
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh")
	argsPath := filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + remote.SingleQuote(argsPath) + "\nexit 0\n"
	if err := os.WriteFile(sshPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	identity := filepath.Join(dir, "id_agent_ed25519")
	if err := os.WriteFile(identity, []byte("private"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(identity+".pub", []byte("ssh-ed25519 AAAATEST"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("TARGET_PASS", "secret")

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"key-deploy", "--host", "example.com", "--identity", identity, "--password-env", "TARGET_PASS", "--jump", "bastion.example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(args), "-J") || !strings.Contains(string(args), "bastion.example.com") {
		t.Fatalf("ssh args missing jump host: %q", string(args))
	}
}
