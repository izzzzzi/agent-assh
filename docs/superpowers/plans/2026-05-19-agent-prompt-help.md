# Agent Prompt Help Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `assh --help` return an LLM-agent JSON manifest and add `assh prompt` as the plain-text companion instruction.

**Architecture:** Keep prompt/manifest content in a focused CLI file so root command wiring stays small. Override root help behavior to emit the manifest through the existing JSON response helper, and keep operational commands unchanged.

**Tech Stack:** Go 1.22, Cobra v1.8.1, existing `internal/response` JSON helpers, Node.js package checks.

---

## File Structure

- Modify `internal/cli/root_test.go`: add failing tests for JSON root help, `help`, and `prompt`.
- Create `internal/cli/prompt.go`: own the agent prompt text, JSON manifest builder, root help writer, and `prompt` command.
- Modify `internal/cli/root.go`: wire root help override and add the `prompt` command.
- Modify `package.json`: include prompt markdown files in npm package contents.
- Modify `scripts/smoke-test.js`: assert `package.json` includes the prompt markdown files.

### Task 1: Root Help Manifest Tests

**Files:**
- Modify: `internal/cli/root_test.go`

- [ ] **Step 1: Add failing root help JSON tests**

Append these tests to `internal/cli/root_test.go`:

```go
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
	if got["agent_prompt"] == "" {
		t.Fatalf("expected agent_prompt in manifest: %#v", got)
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
```

- [ ] **Step 2: Add the missing test import**

Update the import block in `internal/cli/root_test.go`:

```go
import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
go test ./internal/cli
```

Expected: FAIL because `--help` still emits standard Cobra text or because the new `strings` import is unused until Step 1 is present.

### Task 2: Prompt Command Tests

**Files:**
- Modify: `internal/cli/root_test.go`

- [ ] **Step 1: Add failing prompt command tests**

Append these tests to `internal/cli/root_test.go`:

```go
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
		"Use `assh` for SSH work.",
		"assh connect-info --file TMP -n NAME",
		"Never put passwords in command arguments",
		"Use the returned sid and next_commands",
		"assh session read -s SID --seq 1 --limit 50",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt missing %q in %s", want, body)
		}
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/cli
```

Expected: FAIL with an unknown `prompt` command.

### Task 3: Prompt and Manifest Implementation

**Files:**
- Create: `internal/cli/prompt.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Create prompt implementation**

Create `internal/cli/prompt.go`:

```go
package cli

import (
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/spf13/cobra"
)

const agentPrompt = `Use ` + "`assh`" + ` for SSH work.

If the user pasted a provider server-info block, save the full block to a mode 0600 temporary file, run:
assh connect-info --file TMP -n NAME
Then remove TMP.

If connect-info cannot parse the block, extract host, user, and password yourself. Put the password in an environment variable and run:
assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME

Never put passwords in command arguments. Never print, log, repeat, or summarize passwords.

Use the returned sid and next_commands for all remote work:
assh session exec -s SID -- "pwd"
assh session read -s SID --seq 1 --limit 50
assh session read -s SID --seq 1 --stream stderr --limit 50
assh session close -s SID

Keep large remote output out of context. Read bounded windows with --limit, --offset, and --stream. Use --raw only for piping or exact output.
`

func agentHelpManifest() response.OK {
	return response.OK{
		"ok":                   true,
		"tool":                 "assh",
		"version":              version,
		"audience":             "llm_agent",
		"purpose":              "SSH workflow helper for LLM agents",
		"agent_prompt_command": "assh prompt",
		"docs": []string{
			"AGENT_INSTRUCTIONS.md",
			"SYSTEM_PROMPT_snippet.md",
		},
		"agent_prompt": agentPrompt,
		"safety_rules": []string{
			"Never put passwords in command arguments.",
			"Prefer connect-info --file for pasted provider server-info blocks.",
			"Remove temporary server-info files after connect.",
			"Use returned sid and next_commands for remote work.",
			"Read large output with bounded session read windows.",
		},
		"workflow": []string{
			"For pasted provider server-info, write the full block to a mode 0600 temporary file.",
			"Run assh connect-info --file TMP -n NAME, then remove TMP.",
			"If parsing fails, extract host, user, and password; put the password in an environment variable.",
			"Run assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME.",
			"Continue with returned sid and next_commands.",
			"Use assh session exec and assh session read with explicit limits.",
		},
		"commands": response.OK{
			"prompt":       "assh prompt",
			"connect_info": "assh connect-info --file TMP -n NAME",
			"connect":      "assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME",
			"session_exec": "assh session exec -s SID -- \"pwd\"",
			"session_read": "assh session read -s SID --seq 1 --limit 50",
			"session_close": "assh session close -s SID",
		},
		"json_contract": response.OK{
			"operational_commands_emit_json": true,
			"raw_read_commands_emit_content": true,
			"errors_use_ok_false":            true,
		},
	}
}

func writeAgentHelp(cmd *cobra.Command) {
	_ = writeJSON(cmd, agentHelpManifest())
}

func newPromptCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "prompt",
		Short:         "Print minimal LLM-agent usage instructions",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := cmd.OutOrStdout().Write([]byte(agentPrompt))
			return err
		},
	}
}
```

- [ ] **Step 2: Wire root help and prompt command**

In `internal/cli/root.go`, after creating `cmd` and before adding commands, set the help function and add `newPromptCommand()`:

```go
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		writeAgentHelp(cmd.Root())
	})
```

Update the `cmd.AddCommand(...)` list to include:

```go
		newPromptCommand(),
```

- [ ] **Step 3: Run gofmt**

Run:

```bash
gofmt -w internal/cli/root.go internal/cli/root_test.go internal/cli/prompt.go
```

Expected: no output.

- [ ] **Step 4: Run CLI tests**

Run:

```bash
go test ./internal/cli
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add internal/cli/root.go internal/cli/root_test.go internal/cli/prompt.go
git commit -m "feat: add agent help manifest"
```

Expected: commit succeeds after pre-commit checks.

### Task 4: NPM Package Prompt Files

**Files:**
- Modify: `package.json`
- Modify: `scripts/smoke-test.js`

- [ ] **Step 1: Add failing package file assertions**

In `scripts/smoke-test.js`, after `const nativeDir = path.join(root, 'native');`, add:

```js
assert.ok(pkg.files.includes('AGENT_INSTRUCTIONS.md'), 'package files must include AGENT_INSTRUCTIONS.md');
assert.ok(pkg.files.includes('SYSTEM_PROMPT_snippet.md'), 'package files must include SYSTEM_PROMPT_snippet.md');
```

- [ ] **Step 2: Run smoke test to verify it fails**

Run:

```bash
npm run smoke
```

Expected: FAIL with `package files must include AGENT_INSTRUCTIONS.md`.

- [ ] **Step 3: Include prompt files in npm package**

Update the `files` array in `package.json` to include the prompt markdown files:

```json
  "files": [
    "bin/assh.js",
    "scripts",
    "README.md",
    "README.en.md",
    "AGENT_INSTRUCTIONS.md",
    "SYSTEM_PROMPT_snippet.md",
    "LICENSE"
  ]
```

- [ ] **Step 4: Run smoke test**

Run:

```bash
npm run smoke
```

Expected: PASS with `smoke ok`.

- [ ] **Step 5: Verify npm pack includes prompt files**

Run:

```bash
npm pack --dry-run
```

Expected: output lists `AGENT_INSTRUCTIONS.md` and `SYSTEM_PROMPT_snippet.md` in `Tarball Contents`.

- [ ] **Step 6: Commit**

Run:

```bash
git add package.json scripts/smoke-test.js
git commit -m "build: package agent prompt docs"
```

Expected: commit succeeds after pre-commit checks.

### Task 5: Full Verification

**Files:**
- No file changes expected.

- [ ] **Step 1: Run full project check**

Run:

```bash
npm run check
```

Expected: PASS. The command should complete `gofmt -l .`, `go vet ./...`, `go test ./...`, npm smoke, npm pack dry run, and markdownlint with no errors.

- [ ] **Step 2: Manually inspect help output**

Run:

```bash
go run ./cmd/assh --help
```

Expected: stdout is one JSON object. It includes `"ok":true`, `"audience":"llm_agent"`, `"agent_prompt_command":"assh prompt"`, and command examples for `connect-info`, `session exec`, and `session read`.

- [ ] **Step 3: Manually inspect prompt output**

Run:

```bash
go run ./cmd/assh prompt
```

Expected: stdout is plain text beginning with `Use ` + "`assh`" + ` for SSH work.` and includes the password safety rules.

- [ ] **Step 4: Check git status**

Run:

```bash
git status --short
```

Expected: no output.
