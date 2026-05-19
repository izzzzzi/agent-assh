package cli

import (
	"bytes"
	"encoding/json"
	"strings"
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

func TestVersionCommandReturnsJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != true || got["version"] == "" || got["go_version"] == "" {
		t.Fatalf("unexpected version response: %#v", got)
	}
}

func TestDeprecatedJSONFlagRemainsAccepted(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--json=false", "version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestVersionCommandRejectsArgsWithJSONError(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version", "extra"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != false || got["error"] != "invalid_args" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestRootHelpReturnsAgentManifestJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != true || got["audience"] != "llm_agent" || got["agent_prompt_command"] != "assh prompt" {
		t.Fatalf("unexpected manifest: %#v", got)
	}
	agentPrompt, ok := got["agent_prompt"].(string)
	if !ok || agentPrompt == "" {
		t.Fatalf("expected non-empty agent_prompt in manifest: %#v", got)
	}
}

func TestRootHelpCommandReturnsAgentManifestJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != true || got["tool"] != "assh" {
		t.Fatalf("unexpected manifest: %#v", got)
	}
	agentPrompt, ok := got["agent_prompt"].(string)
	if !ok || agentPrompt == "" {
		t.Fatalf("expected non-empty agent_prompt in manifest: %#v", got)
	}
}

func TestRootHelpManifestIncludesWorkflowCommands(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	body := out.String()
	for _, want := range []string{
		"assh prompt",
		"assh connect-info --file TMP -n NAME",
		"assh session exec -s SID --",
		"assh session read -s SID --seq 1 --limit 50",
		"AGENT_INSTRUCTIONS.md",
		"SYSTEM_PROMPT_snippet.md",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("help manifest missing %q in %s", want, body)
		}
	}
}
