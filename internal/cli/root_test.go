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
	if got["ok"] != false || got["error"] != "invalid_args" {
		t.Fatalf("unexpected response: %#v", got)
	}
	hint, _ := got["hint"].(string)
	if !strings.Contains(hint, "assh --help") {
		t.Fatalf("hint %q should point at the help manifest", hint)
	}
}

func TestUnknownCommandSuggestsNearMiss(t *testing.T) {
	tests := []struct {
		typo string
		want string
	}{
		{"connec", "assh connect"},
		{"trnasfer", "assh transfer"},
		{"sesson", "assh session"},
	}

	for _, tt := range tests {
		t.Run(tt.typo, func(t *testing.T) {
			got := executeUnknownCommand(t, tt.typo)
			hint, _ := got["hint"].(string)
			if !strings.Contains(hint, "did you mean `"+tt.want+"`") {
				t.Fatalf("hint %q should suggest %q", hint, tt.want)
			}
		})
	}
}

func TestUnknownCommandWithoutNearMissDoesNotGuess(t *testing.T) {
	got := executeUnknownCommand(t, "zzzzzzzz")
	hint, _ := got["hint"].(string)
	if strings.Contains(hint, "did you mean") {
		t.Fatalf("hint %q must not suggest an arbitrary command", hint)
	}
	if !strings.Contains(hint, "assh --help") {
		t.Fatalf("hint %q should still point at the help manifest", hint)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "", 3},
		{"connec", "connect", 1},
		{"trnasfer", "transfer", 2},
		{"abc", "xyz", 3},
	}

	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Fatalf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestPasswordFlagOnExecExplainsScope(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"shorthand", []string{"exec", "-H", "example.com", "-u", "root", "-E", "TARGET_PASS", "--", "echo hi"}},
		{"long form", []string{"exec", "-H", "example.com", "-u", "root", "--password-env", "TARGET_PASS", "--", "echo hi"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executeFlagError(t, tt.args)
			hint, _ := got["hint"].(string)
			// The point of the hint is to stop the caller retrying the same
			// command: it must name where -E lives and the working alternative.
			for _, want := range []string{"first-login", "assh connect", "assh session exec"} {
				if !strings.Contains(hint, want) {
					t.Fatalf("hint %q missing %q", hint, want)
				}
			}
		})
	}
}

func TestUnrelatedUnknownFlagKeepsGenericHint(t *testing.T) {
	got := executeFlagError(t, []string{"exec", "--local", "-H", "example.com", "--", "echo hi"})
	hint, _ := got["hint"].(string)
	if strings.Contains(hint, "first-login") {
		t.Fatalf("hint %q should not mention password scope for an unrelated flag", hint)
	}
	if !strings.Contains(hint, "assh --help") {
		t.Fatalf("hint %q should point at the help manifest", hint)
	}
}

// executeFlagError runs args expecting a flag-parsing failure and returns the
// decoded JSON error.
func executeFlagError(t *testing.T, args []string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error for %v", args)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["error"] != "invalid_args" {
		t.Fatalf("unexpected response: %#v", got)
	}
	return got
}

func TestRootLsAliasListsSessions(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"ls"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != true || got["count"] != float64(1) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestSessionLsAliasListsSessions(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"session", "ls"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != true || got["count"] != float64(1) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestScanHostsAliasResolves(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"hosts"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error: scan requires a host")
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	// Reaching scan's own validation proves the alias resolved; "unknown command"
	// would mean it did not.
	if got["message"] != "host required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

// executeUnknownCommand runs the root command with an unknown subcommand and
// returns the decoded JSON error.
func executeUnknownCommand(t *testing.T, name string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{name})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error for %q", name)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	return got
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
		"dangerous_command_requires_confirmation",
		"--confirm-danger",
		"assh session read -s SID --seq 1 --limit 50",
		"AGENT_INSTRUCTIONS.md",
		"SYSTEM_PROMPT_snippet.md",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("help manifest missing %q in %s", want, body)
		}
	}
}

func TestSessionHelpReturnsCommandUsage(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"session", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	body := out.String()
	if !strings.Contains(body, "Usage:") || !strings.Contains(body, "assh session") {
		t.Fatalf("expected session usage help, got %q", body)
	}
	if strings.Contains(body, `"audience":"llm_agent"`) {
		t.Fatalf("expected command usage help, got agent manifest %q", body)
	}
}

func TestPromptCommandPrintsAgentInstructions(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"prompt"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	body := out.String()
	for _, want := range []string{
		"For any SSH work use `assh`",
		"assh connect-info --file FILE -n NAME",
		"Never put a password in a command argument",
		"assh session read -s SID --seq 3 --offset 50 --limit 50",
		"assh session job-status -s SID --job-id JOB",
		"dangerous_command_requires_confirmation",
		"--confirm-danger",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt missing %q in %s", want, body)
		}
	}
}

func TestPromptCommandMentionsSessionList(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	body := out.String()
	if !strings.Contains(body, "assh session list") {
		t.Fatalf("prompt manifest missing session list in %s", body)
	}
}

func TestPromptCommandRejectsArgsWithJSONError(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"prompt", "extra"})

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
