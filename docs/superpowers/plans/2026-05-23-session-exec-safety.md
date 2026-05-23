# Session Exec Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a noninteractive safety gate for destructive `assh session exec` commands, requiring `--confirm-danger` before sending them to the remote tmux session.

**Architecture:** Add a focused `internal/safety` package that classifies user command text before the session wrapper is built. Integrate it into `newSessionExecCommand` before `Seq` is incremented and before `runSSH` can be called. Update agent docs so agents know to treat blocked commands as requiring explicit user intent.

**Tech Stack:** Go, Cobra CLI, existing JSON error helpers, existing Go test suite, markdown docs.

---

## File Structure

- Create `internal/safety/safety.go`: command tokenizer and destructive-command classifier.
- Create `internal/safety/safety_test.go`: unit coverage for all built-in rules and safe false-positive cases.
- Modify `internal/cli/session.go`: add `--confirm-danger` and call `safety.CheckCommand` before sequence mutation.
- Modify `internal/cli/session_test.go`: CLI regression tests for blocked commands, preserved `Seq`, skipped SSH, and confirmed execution.
- Modify `README.md`, `README.en.md`, `AGENT_INSTRUCTIONS.md`, `SYSTEM_PROMPT_snippet.md`, and `internal/cli/prompt.go`: document the new flag and agent behavior.

## Task 1: Safety Package Tests

**Files:**
- Create: `internal/safety/safety_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/safety/safety_test.go`:

```go
package safety

import "testing"

func TestCheckCommandBlocksDangerousCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		rule    string
	}{
		{name: "rm recursive", command: "rm -rf /tmp/build", rule: "rm_recursive"},
		{name: "sudo rm recursive", command: "sudo rm -rf /var/www", rule: "rm_recursive"},
		{name: "rm critical path", command: "rm /etc/passwd", rule: "rm_critical_path"},
		{name: "find delete", command: "find /tmp -type f -delete", rule: "find_delete"},
		{name: "mkfs", command: "mkfs.ext4 /dev/sdb", rule: "filesystem_wipe"},
		{name: "wipefs", command: "wipefs -a /dev/sdb", rule: "filesystem_wipe"},
		{name: "shred", command: "shred -u /etc/passwd", rule: "filesystem_wipe"},
		{name: "dd device output", command: "dd if=/dev/zero of=/dev/sda bs=1M", rule: "dd_dangerous_output"},
		{name: "dd absolute output", command: "dd if=/tmp/input of=/etc/passwd", rule: "dd_dangerous_output"},
		{name: "truncate redirect", command: ": > /etc/passwd", rule: "dangerous_redirect"},
		{name: "overwrite redirect", command: "cat /tmp/body > /var/log/app.log", rule: "dangerous_redirect"},
		{name: "chmod recursive", command: "chmod -R 777 /etc", rule: "recursive_permission"},
		{name: "chown recursive", command: "chown -R root:root /var", rule: "recursive_permission"},
		{name: "chgrp recursive", command: "chgrp -R root /srv", rule: "recursive_permission"},
		{name: "compound", command: "pwd && rm -rf /tmp/build", rule: "rm_recursive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CheckCommand(test.command)
			if !got.Dangerous {
				t.Fatalf("CheckCommand(%q) did not report danger", test.command)
			}
			if got.Rule != test.rule {
				t.Fatalf("CheckCommand(%q).Rule = %q, want %q", test.command, got.Rule, test.rule)
			}
			if got.Message == "" {
				t.Fatalf("CheckCommand(%q).Message is empty", test.command)
			}
		})
	}
}

func TestCheckCommandAllowsSafeCommands(t *testing.T) {
	tests := []string{
		`echo "rm -rf /"`,
		"rm file.tmp",
		"ls -la /etc",
		"cat /etc/passwd",
		"grep root /etc/passwd",
		"tail -n 50 /var/log/syslog",
		"journalctl -p warning",
		"printf '> /etc/passwd\n'",
	}

	for _, command := range tests {
		t.Run(command, func(t *testing.T) {
			got := CheckCommand(command)
			if got.Dangerous {
				t.Fatalf("CheckCommand(%q) = %#v, want safe", command, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/safety -run 'TestCheckCommand' -v
```

Expected: FAIL because `internal/safety` does not exist or `CheckCommand` is undefined.

- [ ] **Step 3: Keep red tests uncommitted**

Do not commit the red test state. Continue to Task 2 in the same working tree and commit the tests together with the implementation once they pass.

## Task 2: Safety Package Implementation

**Files:**
- Create: `internal/safety/safety.go`
- Test: `internal/safety/safety_test.go`

- [ ] **Step 1: Implement the classifier**

Create `internal/safety/safety.go`:

```go
package safety

import "strings"

type Result struct {
	Dangerous bool
	Rule      string
	Message   string
}

type token struct {
	Value  string
	Quoted bool
}

func CheckCommand(command string) Result {
	for _, segment := range splitSegments(command) {
		tokens := shellFields(segment)
		if result := checkSegment(tokens); result.Dangerous {
			return result
		}
	}
	return Result{}
}

func checkSegment(tokens []token) Result {
	tokens = commandTokens(tokens)
	if len(tokens) == 0 {
		return Result{}
	}
	name := tokens[0].Value
	args := tokens[1:]

	switch {
	case name == "rm":
		if hasRecursiveFlag(args) {
			return danger("rm_recursive")
		}
		if hasCriticalPath(args) {
			return danger("rm_critical_path")
		}
	case name == "find":
		if hasLiteralArg(args, "-delete") {
			return danger("find_delete")
		}
	case name == "mkfs" || strings.HasPrefix(name, "mkfs.") || name == "wipefs" || name == "shred":
		return danger("filesystem_wipe")
	case name == "dd":
		if hasDangerousDDOutput(args) {
			return danger("dd_dangerous_output")
		}
	case name == "chmod" || name == "chown" || name == "chgrp":
		if hasRecursiveFlag(args) && hasCriticalPath(args) {
			return danger("recursive_permission")
		}
	}

	if hasDangerousRedirect(tokens) {
		return danger("dangerous_redirect")
	}
	return Result{}
}

func danger(rule string) Result {
	return Result{
		Dangerous: true,
		Rule:      rule,
		Message:   "matched destructive pattern: " + rule,
	}
}

func commandTokens(tokens []token) []token {
	for len(tokens) > 0 {
		value := tokens[0].Value
		switch value {
		case "sudo", "command", "builtin":
			tokens = tokens[1:]
		default:
			return tokens
		}
	}
	return tokens
}

func splitSegments(command string) []string {
	var segments []string
	var b strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if strings.TrimSpace(b.String()) != "" {
			segments = append(segments, b.String())
		}
		b.Reset()
	}

	for i := 0; i < len(command); i++ {
		r := rune(command[i])
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			b.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			b.WriteRune(r)
			quote = r
			continue
		}
		if r == ';' || r == '|' {
			flush()
			continue
		}
		if r == '&' && i+1 < len(command) && command[i+1] == '&' {
			flush()
			i++
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return segments
}

func shellFields(segment string) []token {
	var tokens []token
	var b strings.Builder
	var quote rune
	quoted := false
	escaped := false

	flush := func() {
		if b.Len() == 0 && !quoted {
			return
		}
		tokens = append(tokens, token{Value: b.String(), Quoted: quoted})
		b.Reset()
		quoted = false
	}

	for _, r := range segment {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			quoted = true
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return tokens
}

func hasRecursiveFlag(tokens []token) bool {
	for _, tok := range tokens {
		if tok.Quoted || !strings.HasPrefix(tok.Value, "-") {
			continue
		}
		if strings.Contains(tok.Value, "r") || strings.Contains(tok.Value, "R") {
			return true
		}
	}
	return false
}

func hasLiteralArg(tokens []token, value string) bool {
	for _, tok := range tokens {
		if !tok.Quoted && tok.Value == value {
			return true
		}
	}
	return false
}

func hasCriticalPath(tokens []token) bool {
	for _, tok := range tokens {
		if tok.Quoted || strings.HasPrefix(tok.Value, "-") {
			continue
		}
		if criticalPath(tok.Value) {
			return true
		}
	}
	return false
}

func criticalPath(path string) bool {
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}
	if path == "/" || path == "/*" {
		return true
	}
	for _, prefix := range []string{"/etc", "/var", "/home", "/root", "/usr", "/bin", "/sbin", "/lib", "/opt", "/srv"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func hasDangerousDDOutput(tokens []token) bool {
	for _, tok := range tokens {
		if tok.Quoted || !strings.HasPrefix(tok.Value, "of=") {
			continue
		}
		output := strings.TrimPrefix(tok.Value, "of=")
		if strings.HasPrefix(output, "/dev/") || strings.HasPrefix(output, "/") {
			return true
		}
	}
	return false
}

func hasDangerousRedirect(tokens []token) bool {
	for i, tok := range tokens {
		if tok.Quoted || (tok.Value != ">" && tok.Value != "1>") {
			continue
		}
		if i+1 < len(tokens) && !tokens[i+1].Quoted && strings.HasPrefix(tokens[i+1].Value, "/") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run safety tests**

Run:

```bash
go test ./internal/safety -run 'TestCheckCommand' -v
```

Expected: PASS.

- [ ] **Step 3: Run formatting**

Run:

```bash
gofmt -w internal/safety/safety.go internal/safety/safety_test.go
```

- [ ] **Step 4: Commit safety package**

Run:

```bash
git add internal/safety/safety.go internal/safety/safety_test.go
git commit -m "Add session exec safety classifier"
```

Expected: commit succeeds after pre-commit checks.

## Task 3: CLI Safety Gate

**Files:**
- Modify: `internal/cli/session.go`
- Modify: `internal/cli/session_test.go`
- Test: `internal/cli/session_test.go`

- [ ] **Step 1: Write failing CLI tests**

Add these tests after `TestSessionExecRequiresSIDAndCommand` in `internal/cli/session_test.go`:

```go
func TestSessionExecBlocksDangerousCommandWithoutConfirmation(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		t.Fatalf("runSSH called for blocked dangerous command")
		return transport.Result{}
	}

	got := executeSessionJSONError(t, []string{"session", "exec", "--sid", "abcdef12", "--", "rm", "-rf", "/tmp/build"})
	if got["error"] != "dangerous_command_requires_confirmation" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["hint"] != "matched destructive pattern: rm_recursive" {
		t.Fatalf("unexpected hint: %#v", got)
	}

	entry, err := session.LoadRegistry(stateBaseDir(), "abcdef12")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if entry.Seq != 0 {
		t.Fatalf("blocked command incremented seq to %d", entry.Seq)
	}
}

func TestSessionExecConfirmDangerAllowsDangerousCommand(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	called := false
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, _ transport.SSHCommand, remoteCommand string) transport.Result {
		called = true
		if !strings.Contains(remoteCommand, "rm -rf /tmp/build") {
			t.Fatalf("remote command missing user command: %s", remoteCommand)
		}
		return transport.Result{Stdout: []byte("__ASSH_RC__=0\n__ASSH_STDOUT_LINES__=0\n__ASSH_STDERR_LINES__=0\n"), ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{"session", "exec", "--sid", "abcdef12", "--confirm-danger", "--", "rm", "-rf", "/tmp/build"})
	if got["ok"] != true || got["seq"] != float64(1) {
		t.Fatalf("unexpected response: %#v", got)
	}
	if !called {
		t.Fatalf("runSSH was not called")
	}
}

func TestSessionExecDoesNotBlockQuotedDangerousText(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{Stdout: []byte("__ASSH_RC__=0\n__ASSH_STDOUT_LINES__=1\n__ASSH_STDERR_LINES__=0\n"), ExitCode: 0}
	}

	got := executeSessionJSON(t, []string{"session", "exec", "--sid", "abcdef12", "--", "echo", `"rm -rf /"`})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
}
```

- [ ] **Step 2: Run CLI tests to verify failure**

Run:

```bash
go test ./internal/cli -run 'TestSessionExecBlocksDangerousCommandWithoutConfirmation|TestSessionExecConfirmDangerAllowsDangerousCommand|TestSessionExecDoesNotBlockQuotedDangerousText' -v
```

Expected: FAIL because `--confirm-danger` is unknown and no safety gate exists.

- [ ] **Step 3: Integrate safety into `session exec`**

Modify imports in `internal/cli/session.go`:

```go
import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/ids"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/safety"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)
```

Modify `newSessionExecCommand`:

```go
func newSessionExecCommand() *cobra.Command {
	var sid string
	var timeout int
	var confirmDanger bool
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "exec -- command",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if len(args) == 0 {
				return writeInvalidArgs(cmd, "command required", "")
			}
			if timeout < 1 {
				return writeInvalidArgs(cmd, "timeout must be greater than 0", "")
			}
			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}
			userCommand := remoteCommand(args)
			if result := safety.CheckCommand(userCommand); result.Dangerous && !confirmDanger {
				return writeError(cmd, "dangerous_command_requires_confirmation", "command looks destructive; rerun with --confirm-danger if intentional", result.Message)
			}
			entry.Seq++
			remoteCommand, err := session.ExecRemoteCommand(entry.SID, entry.TmuxName, entry.Seq, userCommand, timeout)
			if err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			localTimeout := time.Duration(timeout+5) * time.Second
			ctx, cancel := context.WithTimeout(cmd.Context(), localTimeout)
			defer cancel()
			if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, firstNonEmpty(ssh.Jump, entry.Jump), timeout+5, entry.HostKeyPolicy), remoteCommand)
			if strings.Contains(string(result.Stdout), "__ASSH_TIMEOUT__") {
				return writeError(cmd, "timeout", "session command timed out", "")
			}
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			rc, stdoutLines, stderrLines, timedOut := parseSessionExec(result.Stdout)
			if timedOut {
				return writeError(cmd, "timeout", "session command timed out", "")
			}
			writeAudit("session_exec", entry.SID, entry.Host, entry.User, remoteCommand, rc, stdoutLines, stderrLines)

			return writeJSON(cmd, response.OK{
				"ok":           true,
				"rc":           rc,
				"seq":          entry.Seq,
				"stdout_lines": stdoutLines,
				"stderr_lines": stderrLines,
				"sid":          sid,
				"session":      entry.Label,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 300, "timeout in seconds")
	cmd.Flags().BoolVar(&confirmDanger, "confirm-danger", false, "allow a command that matches destructive safety rules")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}
```

- [ ] **Step 4: Format and run focused CLI tests**

Run:

```bash
gofmt -w internal/cli/session.go internal/cli/session_test.go
go test ./internal/cli -run 'TestSessionExecBlocksDangerousCommandWithoutConfirmation|TestSessionExecConfirmDangerAllowsDangerousCommand|TestSessionExecDoesNotBlockQuotedDangerousText|TestSessionExecReturnsJSON' -v
```

Expected: PASS.

- [ ] **Step 5: Run safety and CLI package tests**

Run:

```bash
go test ./internal/safety ./internal/cli
```

Expected: PASS.

- [ ] **Step 6: Commit CLI integration**

Run:

```bash
git add internal/cli/session.go internal/cli/session_test.go
git commit -m "Add confirmation gate for dangerous session exec"
```

Expected: commit succeeds after pre-commit checks.

## Task 4: Documentation and Agent Prompt Updates

**Files:**
- Modify: `README.md`
- Modify: `README.en.md`
- Modify: `AGENT_INSTRUCTIONS.md`
- Modify: `SYSTEM_PROMPT_snippet.md`
- Modify: `internal/cli/prompt.go`
- Test: `internal/cli/root_test.go`

- [ ] **Step 1: Update README security sections**

In `README.md`, add this bullet under `## Безопасность`:

```markdown
- `session exec` блокирует явно destructive-команды вроде `rm -rf`, `find ... -delete`, `mkfs`, `wipefs`, опасный `dd` и recursive permission changes; для намеренного запуска нужен `--confirm-danger`.
```

In `README.en.md`, add this bullet under `## Security`:

```markdown
- `session exec` blocks clearly destructive commands such as `rm -rf`, `find ... -delete`, `mkfs`, `wipefs`, dangerous `dd`, and recursive permission changes; intentional runs require `--confirm-danger`.
```

- [ ] **Step 2: Update agent instructions**

In `AGENT_INSTRUCTIONS.md`, add this bullet under `## Security Rules`:

```markdown
- If `session exec` returns `dangerous_command_requires_confirmation`, do not add `--confirm-danger` unless the user explicitly intended the destructive action.
```

In `SYSTEM_PROMPT_snippet.md`, add this rule under the existing `Rules:` list:

```markdown
- If `session exec` returns `dangerous_command_requires_confirmation`, ask for explicit user intent before rerunning with `--confirm-danger`.
```

- [ ] **Step 3: Update `assh prompt` manifest**

Modify `agentPrompt` in `internal/cli/prompt.go` by adding this sentence after the session command examples:

```text
If session exec returns dangerous_command_requires_confirmation, do not add --confirm-danger unless the user explicitly intended the destructive action.
```

Add this string to `safety_rules`:

```go
"Do not add --confirm-danger unless the user explicitly intended the destructive action.",
```

- [ ] **Step 4: Update prompt tests**

Modify `TestPromptCommandPrintsAgentInstructions` in `internal/cli/root_test.go` so its `want` list includes the new guidance:

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
		"assh session read -s SID --seq 1 --limit 50 --raw",
		"dangerous_command_requires_confirmation",
		"--confirm-danger",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt missing %q in %s", want, body)
		}
	}
}
```

Add a focused assertion to `TestRootHelpManifestIncludesWorkflowCommands`:

```go
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
		"--confirm-danger",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("help manifest missing %q in %s", want, body)
		}
	}
}
```

Run:

```bash
go test ./internal/cli -run 'TestPrompt|TestRoot|TestAgent' -v
```

Expected: PASS.

- [ ] **Step 5: Run markdown and CLI checks**

Run:

```bash
go test ./internal/cli
npx --yes markdownlint-cli2 --config .markdownlint-cli2.yaml README.md README.en.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
```

Expected: PASS and markdown summary reports `0 error(s)`.

- [ ] **Step 6: Commit docs**

Run:

```bash
git add README.md README.en.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md internal/cli/prompt.go internal/cli/root_test.go
git commit -m "Document session exec danger confirmation"
```

Expected: commit succeeds after pre-commit checks.

## Task 5: Final Verification

**Files:**
- Verify all changed files.

- [ ] **Step 1: Run full Go tests**

Run:

```bash
go test -count=1 ./...
```

Expected: PASS for every package.

- [ ] **Step 2: Run vet**

Run:

```bash
go vet ./...
```

Expected: no output and exit code 0.

- [ ] **Step 3: Run diff check**

Run:

```bash
git diff --check
```

Expected: no output and exit code 0.

- [ ] **Step 4: Inspect final state**

Run:

```bash
git status --short
git log --oneline -5
```

Expected: working tree clean, recent commits include:

```text
Document session exec danger confirmation
Add confirmation gate for dangerous session exec
Add session exec safety classifier
```

- [ ] **Step 5: Optional review**

If requested, run CodeRabbit CLI against the new commits and summarize any findings before making more changes.
