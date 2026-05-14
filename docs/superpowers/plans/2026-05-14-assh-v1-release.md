# assh v1.0 Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the first stable `assh` v1.0 release with the full documented agent workflow, bilingual docs, CI/linting, GoReleaser artifacts, and npm package `agent-assh` that installs command `assh`.

**Architecture:** Keep the current Go package layout and strengthen the CLI contract in place. Use small test seams around SSH execution and state paths so lifecycle behavior can be tested without real SSH. GitHub Releases remain the source of binary artifacts; npm is a thin downloader/wrapper.

**Tech Stack:** Go 1.22+, Cobra, system OpenSSH, GoReleaser, GitHub Actions, npm/Node.js for installer wrapper, golangci-lint, markdownlint-cli2.

---

## File Structure

- Modify `internal/cli/root.go`: add version command and richer command descriptions.
- Modify `internal/cli/exec.go`: use testable SSH runner/state path helpers; add raw output support for `read`; tighten lifecycle error helpers.
- Modify `internal/cli/session.go`: add `--timeout` and `--raw`; make `session open` fail safely; implement remote-aware `session gc`.
- Modify `internal/cli/capabilities.go`: use shared lifecycle failure handling.
- Modify `internal/cli/misc.go`: add audit filters; make scan/key-deploy use shared lifecycle failure handling where applicable.
- Modify `internal/session/service.go`: add remote commands for tmux detection, safe GC candidates, and configurable exec wait.
- Modify `internal/audit/audit.go`: add reader/filter helpers.
- Add `internal/cli/version.go`: version variables and version response.
- Add or modify tests in `internal/cli/*_test.go`, `internal/session/service_test.go`, and `internal/audit/audit_test.go`.
- Add `.goreleaser.yaml`: release builds and archives.
- Add `.golangci.yml`: pragmatic v1 lint profile.
- Add `.markdownlint-cli2.yaml`: docs lint config.
- Add `.github/workflows/ci.yml`: tests, lint, release check, npm smoke.
- Add `.github/workflows/release.yml`: tag-driven GoReleaser and npm publish.
- Add `package.json`, `bin/assh.js`, `scripts/install.js`, `scripts/platform.js`, `scripts/smoke-test.js`, `.npmignore`: npm installer/wrapper.
- Add `LICENSE`: MIT license badge target.
- Rewrite `README.md`: English release README.
- Add `README.ru.md`: Russian release README.
- Update `AGENT_INSTRUCTIONS.md` and `SYSTEM_PROMPT_snippet.md`: agent-facing contract.

---

## Task 1: Version Command and Test Seams

**Files:**
- Create: `internal/cli/version.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/exec.go`
- Modify: `internal/cli/session.go`
- Modify: `internal/cli/capabilities.go`
- Modify: `internal/cli/misc.go`
- Test: `internal/cli/root_test.go`

- [ ] **Step 1: Write failing version command test**

Append this test to `internal/cli/root_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./internal/cli -run TestVersionCommandReturnsJSON
```

Expected: FAIL because `version` command does not exist.

- [ ] **Step 3: Add version command**

Create `internal/cli/version.go`:

```go
package cli

import (
	"runtime"

	"github.com/agent-ssh/assh/internal/response"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print version information as JSON",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeJSON(cmd, response.OK{
				"ok":         true,
				"version":    version,
				"commit":     commit,
				"date":       date,
				"go_version": runtime.Version(),
			})
		},
	}
}
```

Modify `internal/cli/root.go` so `cmd.AddCommand(...)` includes `newVersionCommand()`. Add `Short` descriptions for the root and all command constructors touched in later tasks; use this exact root setup:

```go
cmd := &cobra.Command{
	Use:           "assh",
	Short:         "SSH workflow helper for LLM agents",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return writeInvalidArgs(cmd, "unknown command "+args[0], "run assh --help")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return writeInvalidArgs(cmd, "command required", "run assh --help")
	},
}
```

- [ ] **Step 4: Add CLI test seams**

Add these helpers near the bottom of `internal/cli/exec.go`:

```go
var runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
	return command.Run(ctx, remoteCommand)
}

func stateBaseDir() string {
	return state.BaseDir()
}
```

Replace direct calls like:

```go
transport.SSHCommand{...}.Run(ctx, remoteCommand(args))
```

with:

```go
runSSH(ctx, transport.SSHCommand{...}, remoteCommand(args))
```

Replace `state.BaseDir()` inside `internal/cli/*.go` with `stateBaseDir()` so tests can use `ASSH_STATE_DIR` consistently.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/cli
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli
git commit -m "feat: add version command and cli test seams"
```

---

## Task 2: Raw Read for Stored Output

**Files:**
- Modify: `internal/cli/exec.go`
- Test: `internal/cli/exec_test.go`

- [ ] **Step 1: Write failing tests for `read --raw`**

Append to `internal/cli/exec_test.go`:

```go
func TestReadRawPrintsOnlyContent(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	store := state.NewOutputStore(filepath.Join(stateBaseDir(), "outputs"))
	if err := store.Write("abcdef12", []byte("one\ntwo\nthree\n"), []byte("err\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"read", "--id", "abcdef12", "--offset", "1", "--limit", "1", "--raw"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); got != "two\n" {
		t.Fatalf("raw output = %q, want %q", got, "two\n")
	}
}

func TestReadRawStderr(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	store := state.NewOutputStore(filepath.Join(stateBaseDir(), "outputs"))
	if err := store.Write("abcdef12", []byte("out\n"), []byte("err-a\nerr-b\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"read", "--id", "abcdef12", "--stream", "stderr", "--limit", "2", "--raw"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); got != "err-a\nerr-b\n" {
		t.Fatalf("raw stderr = %q", got)
	}
}
```

Add imports if missing:

```go
import (
	"path/filepath"

	"github.com/agent-ssh/assh/internal/state"
)
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/cli -run 'TestReadRaw'
```

Expected: FAIL because `--raw` is not defined.

- [ ] **Step 3: Implement `--raw`**

In `newReadCommand`, add:

```go
var raw bool
```

Register flag:

```go
cmd.Flags().BoolVar(&raw, "raw", false, "print only content without JSON")
```

After `page, err := store.Read(...)`, before `writeJSON`:

```go
if raw {
	_, _ = cmd.OutOrStdout().Write([]byte(page.Content))
	return nil
}
```

Use `filepath.Join(stateBaseDir(), "outputs")` for the store path.

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/cli -run 'TestReadRaw|TestRead'
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/exec.go internal/cli/exec_test.go
git commit -m "feat: add raw output reads"
```

---

## Task 3: Lifecycle Error Policy and Safe Session Open

**Files:**
- Modify: `internal/cli/exec.go`
- Modify: `internal/cli/session.go`
- Modify: `internal/session/service.go`
- Test: `internal/cli/session_test.go`
- Test: `internal/session/service_test.go`

- [ ] **Step 1: Write failing tests for `tmux_missing` and registry safety**

Append to `internal/cli/session_test.go`:

```go
func TestSessionOpenTmuxMissingDoesNotSaveRegistry(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stdout:   nil,
			Stderr:   []byte("tmux_missing\n"),
			ExitCode: 127,
			Err:      &exec.ExitError{},
		}
	}

	got := executeJSONError(t, []string{"session", "open", "--host", "example.com", "--name", "deploy"})
	if got["error"] != "tmux_missing" {
		t.Fatalf("unexpected response: %#v", got)
	}

	entries, err := session.ListRegistry(stateBaseDir())
	if err != nil {
		t.Fatalf("ListRegistry() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("registry saved despite remote failure: %#v", entries)
	}
}
```

Add imports if missing:

```go
import (
	"context"
	"os/exec"

	"github.com/agent-ssh/assh/internal/session"
	"github.com/agent-ssh/assh/internal/transport"
)
```

- [ ] **Step 2: Write failing session command generation test**

Append to `internal/session/service_test.go`:

```go
func TestOpenRemoteCommandChecksTmuxBeforeCreatingSession(t *testing.T) {
	meta := NewMetadata("abcdef12", "deploy", time.Hour, "")
	body, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	cmd, err := OpenRemoteCommand(string(body), meta.TmuxName)
	if err != nil {
		t.Fatalf("OpenRemoteCommand() error = %v", err)
	}
	if !strings.Contains(cmd, "command -v tmux") {
		t.Fatalf("remote open command does not check tmux: %s", cmd)
	}
	if strings.Index(cmd, "command -v tmux") > strings.Index(cmd, "tmux new-session") {
		t.Fatalf("tmux check must happen before tmux new-session: %s", cmd)
	}
}
```

Add imports if missing:

```go
import (
	"encoding/json"
	"strings"
	"time"
)
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/session -run TestOpenRemoteCommandChecksTmuxBeforeCreatingSession
go test ./internal/cli -run TestSessionOpenTmuxMissingDoesNotSaveRegistry
```

Expected: FAIL.

- [ ] **Step 4: Implement lifecycle error mapping**

Add this helper to `internal/cli/exec.go`:

```go
func lifecycleResultErrorCode(ctxErr error, result transport.Result) string {
	if code := sshResultErrorCode(ctxErr, result); code != "" {
		return code
	}
	if result.Err == nil && result.ExitCode == 0 {
		return ""
	}
	stderr := strings.ToLower(strings.TrimSpace(string(result.Stderr)))
	switch {
	case strings.Contains(stderr, "tmux_missing"):
		return "tmux_missing"
	case strings.Contains(stderr, "tmux_install_failed"):
		return "tmux_install_failed"
	default:
		return "command_failed"
	}
}
```

Use `lifecycleResultErrorCode` instead of `sshResultErrorCode` in lifecycle commands: `session open`, `session close`, `capabilities`, `scan`, and `key-deploy` where the command result represents setup/probe workflow success.

- [ ] **Step 5: Make `session open` check tmux and save registry only after success**

In `internal/session/service.go`, update `OpenRemoteCommand` parts to include the check first:

```go
parts := []string{
	"command -v tmux >/dev/null 2>&1 || { echo tmux_missing >&2; exit 127; }",
	"mkdir -p " + sessionRoot,
	"mkdir -p " + sessionDir,
	"printf %s " + remote.SingleQuote(metaJSON) + " > " + metaPath,
	"tmux new-session -d -s " + remote.SingleQuote(tmuxName),
}
```

In `internal/cli/session.go`, keep the existing `SaveRegistry` call after remote execution and error handling. Ensure the error branch happens before `SaveRegistry`.

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./internal/session ./internal/cli
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli internal/session
git commit -m "fix: enforce session lifecycle failures"
```

---

## Task 4: Session Exec Timeout and Raw Session Read

**Files:**
- Modify: `internal/cli/session.go`
- Modify: `internal/session/service.go`
- Test: `internal/cli/session_test.go`
- Test: `internal/session/service_test.go`

- [ ] **Step 1: Write failing tests for configurable timeout**

Append to `internal/session/service_test.go`:

```go
func TestExecRemoteCommandUsesConfiguredWaitSeconds(t *testing.T) {
	cmd, err := ExecRemoteCommand("abcdef12", "assh_abcdef12", 1, "true", 7)
	if err != nil {
		t.Fatalf("ExecRemoteCommand() error = %v", err)
	}
	if !strings.Contains(cmd, "while [ $i -lt 7 ]") {
		t.Fatalf("command does not use configured wait: %s", cmd)
	}
}
```

- [ ] **Step 2: Write failing test for `session read --raw`**

Append to `internal/cli/session_test.go`:

```go
func TestSessionReadRawPrintsOnlyContent(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	entry := session.RegistryEntry{
		SID:           "abcdef12",
		Label:         "deploy",
		Host:          "example.com",
		User:          "root",
		Port:          22,
		HostKeyPolicy: "accept-new",
		TmuxName:      "assh_abcdef12",
		CreatedAt:     time.Now().UTC(),
		TTLSeconds:    3600,
	}
	if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
		return transport.Result{
			Stdout:   []byte("line-a\nline-b\n\n__ASSH_TOTAL_LINES__=2\n"),
			ExitCode: 0,
		}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"session", "read", "--sid", "abcdef12", "--seq", "1", "--raw"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); got != "line-a\nline-b\n" {
		t.Fatalf("raw session output = %q", got)
	}
}
```

Add imports if missing:

```go
import (
	"bytes"
	"time"
)
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/session -run TestExecRemoteCommandUsesConfiguredWaitSeconds
go test ./internal/cli -run TestSessionReadRawPrintsOnlyContent
```

Expected: FAIL.

- [ ] **Step 4: Change `ExecRemoteCommand` signature**

In `internal/session/service.go`, change:

```go
func ExecRemoteCommand(sid, tmuxName string, seq int, command string) (string, error)
```

to:

```go
func ExecRemoteCommand(sid, tmuxName string, seq int, command string, waitSeconds int) (string, error)
```

Validate:

```go
if waitSeconds < 1 {
	return "", errors.New("timeout must be positive")
}
```

Replace hard-coded `120` with `strconv.Itoa(waitSeconds)`.

- [ ] **Step 5: Add `--timeout` to `session exec`**

In `newSessionExecCommand`, add:

```go
var timeout int
```

Validate:

```go
if timeout < 1 {
	return writeInvalidArgs(cmd, "timeout must be greater than 0", "")
}
```

Register:

```go
cmd.Flags().IntVarP(&timeout, "timeout", "t", 300, "timeout in seconds")
```

Call:

```go
remoteCommand, err := session.ExecRemoteCommand(entry.SID, entry.TmuxName, entry.Seq, remoteCommand(args), timeout)
ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(timeout+5)*time.Second)
```

- [ ] **Step 6: Add `--raw` to `session read`**

In `newSessionReadCommand`, add:

```go
var raw bool
```

Register:

```go
cmd.Flags().BoolVar(&raw, "raw", false, "print only content without JSON")
```

After `content, total, notFound := parseSessionRead(result.Stdout)`, add:

```go
if raw {
	_, _ = cmd.OutOrStdout().Write([]byte(content))
	return nil
}
```

- [ ] **Step 7: Run tests**

Run:

```bash
go test ./internal/session ./internal/cli
go test ./...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/session.go internal/cli/session_test.go internal/session/service.go internal/session/service_test.go
git commit -m "feat: complete session exec and read contract"
```

---

## Task 5: Remote-Aware Session GC

**Files:**
- Modify: `internal/session/service.go`
- Modify: `internal/cli/session.go`
- Test: `internal/session/service_test.go`
- Test: `internal/cli/session_test.go`

- [ ] **Step 1: Write failing command generation tests**

Append to `internal/session/service_test.go`:

```go
func TestGCRemoteCommandValidatesMetadataBeforeDelete(t *testing.T) {
	cmd, err := GCRemoteCommand("abcdef12", "assh_abcdef12")
	if err != nil {
		t.Fatalf("GCRemoteCommand() error = %v", err)
	}
	for _, want := range []string{
		"meta.json",
		"\"created_by\":\"assh\"",
		"tmux kill-session -t 'assh_abcdef12'",
		"rm -rf ~/.assh/sessions/abcdef12",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("GCRemoteCommand missing %q: %s", want, cmd)
		}
	}
}
```

- [ ] **Step 2: Write failing CLI dry-run filter test**

Append to `internal/cli/session_test.go`:

```go
func TestSessionGCDryRunFiltersByHostAndAge(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	now := time.Now().UTC()
	entries := []session.RegistryEntry{
		{SID: "abcdef12", Host: "a.example", User: "root", Port: 22, HostKeyPolicy: "accept-new", TmuxName: "assh_abcdef12", CreatedAt: now.Add(-48 * time.Hour), TTLSeconds: 3600},
		{SID: "abcdef13", Host: "b.example", User: "root", Port: 22, HostKeyPolicy: "accept-new", TmuxName: "assh_abcdef13", CreatedAt: now.Add(-48 * time.Hour), TTLSeconds: 3600},
	}
	for _, entry := range entries {
		if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
			t.Fatalf("SaveRegistry() error = %v", err)
		}
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"session", "gc", "--host", "a.example", "--older-than", "24h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got["dry_run"] != true {
		t.Fatalf("expected dry run: %#v", got)
	}
	candidates := got["candidates"].([]any)
	if len(candidates) != 1 || candidates[0] != "abcdef12" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/session -run TestGCRemoteCommandValidatesMetadataBeforeDelete
go test ./internal/cli -run TestSessionGCDryRunFiltersByHostAndAge
```

Expected: FAIL.

- [ ] **Step 4: Add `GCRemoteCommand`**

Add to `internal/session/service.go`:

```go
func GCRemoteCommand(sid, tmuxName string) (string, error) {
	if err := validateSessionTarget(sid, tmuxName); err != nil {
		return "", err
	}
	dir := sessionDir(sid)
	metaPath := dir + "/meta.json"
	return "test -f " + metaPath + " || exit 0; " +
		"grep -q '\"created_by\":\"assh\"' " + metaPath + " || exit 3; " +
		"grep -q '\"sid\":\"" + sid + "\"' " + metaPath + " || exit 3; " +
		"tmux kill-session -t " + remote.SingleQuote(tmuxName) + " 2>/dev/null || true; " +
		"rm -rf " + dir, nil
}
```

- [ ] **Step 5: Implement CLI flags and execution**

In `newSessionGCCommand`, add:

```go
var host string
var olderThan time.Duration
```

Register:

```go
cmd.Flags().StringVar(&host, "host", "", "filter by host")
cmd.Flags().DurationVar(&olderThan, "older-than", 0, "include sessions older than duration")
```

Candidate rule:

```go
if host != "" && entry.Host != host {
	continue
}
if olderThan > 0 && entry.CreatedAt.After(now.Add(-olderThan)) {
	continue
}
if olderThan == 0 && !(session.Metadata{CreatedAt: entry.CreatedAt, TTLSeconds: entry.TTLSeconds}).Expired(now) {
	continue
}
```

For `--execute`, run `GCRemoteCommand`, classify with `lifecycleResultErrorCode`, delete local registry only if remote cleanup succeeds or remote session is already absent. Return:

```go
response.OK{
	"ok":         true,
	"dry_run":    !execute,
	"candidates": candidates,
	"deleted":    deleted,
	"errors":     cleanupErrors,
}
```

Use `[]string` for `candidates` and `deleted`; use `[]map[string]string` for `errors` with `sid` and `error`.

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./internal/session ./internal/cli
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/session internal/cli/session.go internal/cli/session_test.go
git commit -m "feat: add remote-aware session gc"
```

---

## Task 6: Audit Filters

**Files:**
- Modify: `internal/audit/audit.go`
- Modify: `internal/cli/misc.go`
- Test: `internal/audit/audit_test.go`
- Test: `internal/cli/session_test.go` or `internal/cli/misc_test.go`

- [ ] **Step 1: Add audit filter tests**

Append to `internal/audit/audit_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/audit -run TestReadFiltersAuditEvents
```

Expected: FAIL because `Read` and `Filter` do not exist.

- [ ] **Step 3: Implement audit reader**

Add to `internal/audit/audit.go`:

```go
type Filter struct {
	Last   int
	Host   string
	Failed bool
}

func Read(path string, filter Filter) ([]Event, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, err
		}
		if filter.Host != "" && event.Host != filter.Host {
			continue
		}
		if filter.Failed && event.ExitCode == 0 {
			continue
		}
		events = append(events, event)
	}
	if filter.Last > 0 && len(events) > filter.Last {
		events = events[len(events)-filter.Last:]
	}
	return events, nil
}
```

Add import:

```go
import "strings"
```

- [ ] **Step 4: Wire CLI flags**

In `newAuditCommand`, add:

```go
var host string
var failed bool
```

Register:

```go
cmd.Flags().StringVar(&host, "host", "", "filter by host")
cmd.Flags().BoolVar(&failed, "failed", false, "show only failed events")
```

Replace manual JSON array writing with:

```go
events, err := audit.Read(filepath.Join(stateBaseDir(), "audit", "audit.jsonl"), audit.Filter{
	Last:   last,
	Host:   host,
	Failed: failed,
})
if err != nil {
	return writeError(cmd, "internal_error", err.Error(), "")
}
return writeJSON(cmd, events)
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/audit ./internal/cli
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/audit internal/cli/misc.go
git commit -m "feat: add audit filters"
```

---

## Task 7: Release Configuration

**Files:**
- Create: `.goreleaser.yaml`
- Create: `.golangci.yml`
- Create: `.markdownlint-cli2.yaml`
- Create: `LICENSE`
- Modify: `go.mod` only if required by tooling

- [ ] **Step 1: Add GoReleaser config**

Create `.goreleaser.yaml`:

```yaml
version: 2

project_name: assh

before:
  hooks:
    - go mod tidy

builds:
  - id: assh
    main: ./cmd/assh
    binary: assh
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w
      - -X github.com/agent-ssh/assh/internal/cli.version={{.Version}}
      - -X github.com/agent-ssh/assh/internal/cli.commit={{.Commit}}
      - -X github.com/agent-ssh/assh/internal/cli.date={{.Date}}

archives:
  - id: default
    ids:
      - assh
    name_template: >-
      {{ .ProjectName }}_
      {{- .Version }}_
      {{- .Os }}_
      {{- .Arch }}
    format_overrides:
      - goos: windows
        formats: ["zip"]

checksum:
  name_template: checksums.txt

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
```

- [ ] **Step 2: Add lint configs**

Create `.golangci.yml`:

```yaml
version: "2"

linters:
  enable:
    - errcheck
    - govet
    - ineffassign
    - staticcheck
    - unused
    - misspell

formatters:
  enable:
    - gofmt
```

Create `.markdownlint-cli2.yaml`:

```yaml
config:
  MD013: false
  MD033: false
globs:
  - "*.md"
  - "docs/**/*.md"
```

- [ ] **Step 3: Add MIT license**

Create `LICENSE`:

```text
MIT License

Copyright (c) 2026 assh contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 4: Verify release config**

Run:

```bash
goreleaser check
go test ./...
```

Expected: PASS. If `goreleaser` is not installed, install it with Homebrew or run this check in CI after Task 9.

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml .golangci.yml .markdownlint-cli2.yaml LICENSE
git commit -m "build: add release and lint configuration"
```

---

## Task 8: npm Package Installer and Wrapper

**Files:**
- Create: `package.json`
- Create: `bin/assh.js`
- Create: `scripts/platform.js`
- Create: `scripts/install.js`
- Create: `scripts/smoke-test.js`
- Create: `.npmignore`

- [ ] **Step 1: Create npm package metadata**

Create `package.json`:

```json
{
  "name": "agent-assh",
  "version": "1.0.0",
  "description": "SSH workflow helper for LLM agents",
  "license": "MIT",
  "bin": {
    "assh": "bin/assh.js"
  },
  "scripts": {
    "postinstall": "node scripts/install.js",
    "smoke": "node scripts/smoke-test.js",
    "pack:dry": "npm pack --dry-run"
  },
  "files": [
    "bin",
    "scripts",
    "README.md",
    "README.ru.md",
    "LICENSE"
  ],
  "repository": {
    "type": "git",
    "url": "git+https://github.com/agent-ssh/assh.git"
  },
  "keywords": [
    "ssh",
    "llm",
    "agent",
    "cli"
  ],
  "engines": {
    "node": ">=18"
  }
}
```

- [ ] **Step 2: Add platform mapper**

Create `scripts/platform.js`:

```js
"use strict";

function target(platform = process.platform, arch = process.arch) {
  const osMap = { linux: "linux", darwin: "darwin", win32: "windows" };
  const archMap = { x64: "amd64", arm64: "arm64" };
  const os = osMap[platform];
  const goarch = archMap[arch];
  if (!os || !goarch || (os === "windows" && goarch === "arm64")) {
    throw new Error(`Unsupported platform: ${platform}/${arch}`);
  }
  return {
    os,
    arch: goarch,
    ext: os === "windows" ? ".exe" : "",
    archiveExt: os === "windows" ? ".zip" : ".tar.gz"
  };
}

module.exports = { target };
```

- [ ] **Step 3: Add JS wrapper**

Create `bin/assh.js`:

```js
#!/usr/bin/env node
"use strict";

const { spawnSync } = require("node:child_process");
const path = require("node:path");
const { target } = require("../scripts/platform");

const info = target();
const binary = path.join(__dirname, "..", "vendor", `assh${info.ext}`);
const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
```

- [ ] **Step 4: Add installer**

Create `scripts/install.js`:

```js
"use strict";

const childProcess = require("node:child_process");
const crypto = require("node:crypto");
const fs = require("node:fs");
const https = require("node:https");
const path = require("node:path");
const { target } = require("./platform");

const pkg = require("../package.json");
const info = target();
const version = `v${pkg.version}`;
const base = `https://github.com/agent-ssh/assh/releases/download/${version}`;
const archive = `assh_${pkg.version}_${info.os}_${info.arch}${info.archiveExt}`;
const url = `${base}/${archive}`;
const vendor = path.join(__dirname, "..", "vendor");
const dest = path.join(vendor, `assh${info.ext}`);

function download(fileUrl, output) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(output);
    https.get(fileUrl, (response) => {
      if (response.statusCode !== 200) {
        reject(new Error(`Download failed ${response.statusCode}: ${fileUrl}`));
        return;
      }
      response.pipe(file);
      file.on("finish", () => file.close(resolve));
    }).on("error", reject);
  });
}

function sha256(filePath) {
  const hash = crypto.createHash("sha256");
  hash.update(fs.readFileSync(filePath));
  return hash.digest("hex");
}

function expectedChecksum(checksums, fileName) {
  const line = checksums.split(/\r?\n/).find((entry) => entry.trim().endsWith(`  ${fileName}`) || entry.trim().endsWith(` *${fileName}`));
  if (!line) {
    throw new Error(`Checksum missing for ${fileName}`);
  }
  return line.trim().split(/\s+/)[0];
}

function extract(archivePath) {
  if (info.archiveExt === ".zip") {
    const script = `Expand-Archive -LiteralPath '${archivePath.replace(/'/g, "''")}' -DestinationPath '${vendor.replace(/'/g, "''")}' -Force`;
    childProcess.execFileSync("powershell", ["-NoProfile", "-Command", script], { stdio: "inherit" });
    return;
  }
  childProcess.execFileSync("tar", ["-xzf", archivePath, "-C", vendor], { stdio: "inherit" });
}

async function main() {
  fs.mkdirSync(vendor, { recursive: true });
  const archivePath = path.join(vendor, archive);
  if (process.env.AGENT_ASSH_SKIP_DOWNLOAD === "1") {
    fs.writeFileSync(dest, "#!/bin/sh\nprintf 'assh test binary\\n'\n");
    fs.chmodSync(dest, 0o755);
    return;
  }
  await download(url, archivePath);
  const checksumsPath = path.join(vendor, "checksums.txt");
  await download(`${base}/checksums.txt`, checksumsPath);
  const checksums = fs.readFileSync(checksumsPath, "utf8");
  const expected = expectedChecksum(checksums, archive);
  const actual = sha256(archivePath);
  if (actual !== expected) {
    throw new Error(`Checksum mismatch for ${archive}: expected ${expected}, got ${actual}`);
  }
  extract(archivePath);
  fs.chmodSync(dest, 0o755);
}

main().catch((error) => {
  console.error(error.message);
  process.exit(1);
});
```

- [ ] **Step 5: Add npm smoke test**

Create `scripts/smoke-test.js`:

```js
"use strict";

const assert = require("node:assert");
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { target } = require("./platform");

assert.deepStrictEqual(target("linux", "x64").os, "linux");
assert.deepStrictEqual(target("darwin", "arm64").arch, "arm64");
assert.throws(() => target("freebsd", "x64"), /Unsupported platform/);

const vendor = path.join(__dirname, "..", "vendor");
fs.mkdirSync(vendor, { recursive: true });
const bin = path.join(vendor, process.platform === "win32" ? "assh.exe" : "assh");
fs.writeFileSync(bin, process.platform === "win32" ? "@echo off\r\necho assh smoke\r\n" : "#!/bin/sh\necho assh smoke\n");
fs.chmodSync(bin, 0o755);

const wrapper = path.join(__dirname, "..", "bin", "assh.js");
const result = spawnSync(process.execPath, [wrapper], { encoding: "utf8" });
assert.strictEqual(result.status, 0, result.stderr);
assert.match(result.stdout, /assh smoke/);
```

Create `.npmignore`:

```text
.git
.github
bin/assh
dist
docs/superpowers
*.test
```

- [ ] **Step 6: Run npm tests**

Run:

```bash
chmod +x bin/assh.js
npm run smoke
npm pack --dry-run
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add package.json bin scripts .npmignore
git commit -m "build: add npm installer package"
```

---

## Task 9: GitHub Actions CI and Release Workflows

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Add CI workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: test -z "$(gofmt -l .)"
      - run: go vet ./...
      - run: go test ./...

  race:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go test -race ./...

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest
      - uses: DavidAnson/markdownlint-cli2-action@v17
        with:
          globs: "*.md docs/**/*.md"

  release-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: latest
          args: check

  npm-smoke:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
      - run: npm run smoke
      - run: npm pack --dry-run
```

- [ ] **Step 2: Add release workflow**

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
          registry-url: "https://registry.npmjs.org"
      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      - run: npm publish --access public
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
```

- [ ] **Step 3: Validate YAML and local checks**

Run:

```bash
go test ./...
npm run smoke
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows
git commit -m "ci: add test and release workflows"
```

---

## Task 10: Release Documentation

**Files:**
- Modify: `README.md`
- Create: `README.ru.md`
- Modify: `AGENT_INSTRUCTIONS.md`
- Modify: `SYSTEM_PROMPT_snippet.md`

- [ ] **Step 1: Replace English README**

Rewrite `README.md` with sections:

```markdown
# assh

[![CI](https://github.com/agent-ssh/assh/actions/workflows/ci.yml/badge.svg)](https://github.com/agent-ssh/assh/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/agent-ssh/assh)](https://github.com/agent-ssh/assh/releases)
[![npm](https://img.shields.io/npm/v/agent-assh)](https://www.npmjs.com/package/agent-assh)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

SSH workflow helper for LLM agents.

`assh` keeps large SSH output out of the agent context. Commands return metadata first, and agents read only the lines they need. Persistent sessions use remote `tmux` so `cwd` and environment survive between related commands.

## Install

```bash
npm i -g agent-assh
assh version
```

GitHub Release archives are available for Linux, macOS, and Windows on amd64/arm64 where supported.

## Quick Start

```bash
assh exec -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -- "journalctl -p warning"
assh read --id OUTPUT_ID --limit 20 --offset 0
assh read --id OUTPUT_ID --stream stderr --raw
```

## Persistent Session

```bash
assh session open -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -n deploy
assh session exec -s SID -- "cd /app"
assh session exec -s SID -- "git pull"
assh session read -s SID --seq 2 --limit 20
assh session close -s SID
```

## Commands

- `assh exec`: run one remote command and store output locally.
- `assh read`: read stored output with pagination or `--raw`.
- `assh session open|exec|read|close|gc`: persistent tmux workflow.
- `assh capabilities`: inspect remote session support.
- `assh scan`: return host inventory JSON.
- `assh key-deploy`: deploy an SSH key using a password from env.
- `assh audit`: read local audit events with `--last`, `--host`, and `--failed`.
- `assh version`: print version metadata.

## JSON Contract

Operational commands emit one JSON value by default. Errors use:

```json
{"ok":false,"error":"tmux_missing","message":"tmux is not installed"}
```

`exec` and `session exec` treat remote non-zero status as command results, not transport failures.

## Security

- Passwords are accepted only through environment variables for `key-deploy`.
- Command text is not written to audit logs; audit entries use command hashes.
- Remote cleanup only targets sessions with trusted `assh` metadata.

## Russian

See [README.ru.md](README.ru.md).
```

- [ ] **Step 2: Add Russian README**

Create `README.ru.md`:

```markdown
# assh

[![CI](https://github.com/agent-ssh/assh/actions/workflows/ci.yml/badge.svg)](https://github.com/agent-ssh/assh/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/agent-ssh/assh)](https://github.com/agent-ssh/assh/releases)
[![npm](https://img.shields.io/npm/v/agent-assh)](https://www.npmjs.com/package/agent-assh)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

SSH-инструмент для рабочих процессов LLM-агентов.

`assh` не отправляет большой SSH-вывод в контекст агента. Команды сначала возвращают метаданные, а агент читает только нужные строки. Persistent sessions используют remote `tmux`, поэтому `cwd` и окружение сохраняются между связанными командами.

## Установка

```bash
npm i -g agent-assh
assh version
```

Архивы GitHub Releases доступны для Linux, macOS и Windows на поддерживаемых amd64/arm64 платформах.

## Быстрый старт

```bash
assh exec -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -- "journalctl -p warning"
assh read --id OUTPUT_ID --limit 20 --offset 0
assh read --id OUTPUT_ID --stream stderr --raw
```

## Persistent session

```bash
assh session open -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -n deploy
assh session exec -s SID -- "cd /app"
assh session exec -s SID -- "git pull"
assh session read -s SID --seq 2 --limit 20
assh session close -s SID
```

## Команды

- `assh exec`: выполнить одну remote-команду и сохранить вывод локально.
- `assh read`: прочитать сохранённый вывод с пагинацией или через `--raw`.
- `assh session open|exec|read|close|gc`: persistent workflow через tmux.
- `assh capabilities`: проверить поддержку session workflow на сервере.
- `assh scan`: вернуть JSON-инвентарь хоста.
- `assh key-deploy`: поставить SSH-ключ, используя пароль из env.
- `assh audit`: читать локальный аудит через `--last`, `--host`, `--failed`.
- `assh version`: вывести метаданные версии.

## JSON-контракт

Операционные команды по умолчанию печатают один JSON-объект. Ошибки имеют форму:

```json
{"ok":false,"error":"tmux_missing","message":"tmux is not installed"}
```

`exec` и `session exec` считают ненулевой remote status результатом команды, а не transport failure.

## Безопасность

- Пароли принимаются только через env-переменные в `key-deploy`.
- Текст команд не пишется в audit log; сохраняется hash.
- Remote cleanup удаляет только sessions с доверенной metadata `assh`.

## English

See [README.md](README.md).
```

- [ ] **Step 3: Update agent docs**

Update `AGENT_INSTRUCTIONS.md` and `SYSTEM_PROMPT_snippet.md` to include:

- `npm i -g agent-assh`
- `read --raw`
- `session read --raw`
- `session gc --older-than 24h --execute`
- `audit --last 20 --host HOST --failed`
- no `cwd` response field
- no `attempt` response field

- [ ] **Step 4: Run docs checks**

Run:

```bash
npx markdownlint-cli2 "*.md" "docs/**/*.md"
go run ./cmd/assh --help
go run ./cmd/assh read --help
go run ./cmd/assh session read --help
```

Expected: markdown lint passes; `read --help` and `session read --help` list `--raw`.

- [ ] **Step 5: Commit**

```bash
git add README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
git commit -m "docs: prepare v1 release documentation"
```

---

## Task 11: Final Verification and v1.0 Release Dry Run

**Files:**
- No planned source files. This task verifies the completed release branch.

- [ ] **Step 1: Run full local verification**

Run:

```bash
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go test -race ./...
npm run smoke
npm pack --dry-run
goreleaser check
goreleaser release --snapshot --clean
```

Expected: all commands pass and `dist/` contains snapshot artifacts.

- [ ] **Step 2: Verify CLI examples**

Run:

```bash
go run ./cmd/assh version
go run ./cmd/assh --help
go run ./cmd/assh read --help
go run ./cmd/assh session exec --help
go run ./cmd/assh audit --help
```

Expected: JSON for `version`; help lists documented flags.

- [ ] **Step 3: Inspect release package contents**

Run:

```bash
npm pack --dry-run
ls -la dist
```

Expected: npm package includes only package files; `dist` includes archives and checksums.

- [ ] **Step 4: Commit verification fixes when the previous steps changed files**

When verification produces source, docs, config, or package changes, commit them:

```bash
git add .
git commit -m "chore: finalize v1 release checks"
```

When `git status --short` is empty, skip this step.

- [ ] **Step 5: Tag and publish after remote/secrets are ready**

Before tagging, confirm:

- `git remote -v` points to the intended GitHub repository.
- GitHub repository has `NPM_TOKEN` secret.
- npm package `agent-assh` is available to the logged-in owner.

Run:

```bash
git tag -a v1.0.0 -m "v1.0.0"
git push origin main
git push origin v1.0.0
```

Expected: release workflow publishes GitHub Release artifacts and npm package.
