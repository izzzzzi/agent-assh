# assh Go CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Bash `assh` MVP with a cross-platform Go CLI that exposes stable JSON-first SSH functions for agents.

**Architecture:** Build a Go module with Cobra commands, a small internal JSON response package, local state storage, a subprocess OpenSSH transport, and session lifecycle services around remote `tmux`. Keep the existing Bash script as reference behavior while developing the Go binary as `assh-go`.

**Tech Stack:** Go 1.22+, Cobra, standard `encoding/json`, `os/exec`, `context`, `testing`, mock `ssh` integration tests.

---

## File Structure

- Create `go.mod`: Go module declaration and Cobra dependency.
- Create `cmd/assh-go/main.go`: CLI entrypoint.
- Create `internal/cli/root.go`: root command, global flags, JSON error boundary.
- Create `internal/cli/exec.go`: `exec` and `read` commands.
- Create `internal/cli/session.go`: `session open/exec/read/close/gc` command wiring.
- Create `internal/cli/capabilities.go`: `capabilities` and `scan` command wiring.
- Create `internal/response/response.go`: JSON response and error types.
- Create `internal/ids/ids.go`: secure IDs and validators.
- Create `internal/state/dirs.go`: OS-specific state directories.
- Create `internal/state/output.go`: output storage and pagination.
- Create `internal/transport/ssh.go`: system OpenSSH subprocess backend.
- Create `internal/remote/shell.go`: remote shell quoting helpers.
- Create `internal/session/service.go`: session lifecycle and metadata model.
- Create `internal/capabilities/service.go`: remote capability detection.
- Create `internal/audit/audit.go`: JSONL audit writer without command text.
- Create tests next to each package plus integration tests under `internal/integration`.

Commit steps in this plan assume a git workspace. In the current `/Users/apple/agent_ssh` directory, `git rev-parse --is-inside-work-tree` returns non-zero, so execution must either initialize git first or skip commit commands with a note.

## Task 1: Go Module and JSON Response Contract

**Files:**
- Create: `go.mod`
- Create: `cmd/assh-go/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/response/response.go`
- Test: `internal/response/response_test.go`
- Test: `internal/cli/root_test.go`

- [ ] **Step 1: Write failing response tests**

Create `internal/response/response_test.go`:

```go
package response

import (
	"encoding/json"
	"testing"
)

func TestOKJSON(t *testing.T) {
	body, err := Marshal(OK{"ok": true, "output_id": "abc123", "stdout_lines": 4})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got["ok"] != true || got["output_id"] != "abc123" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestErrorJSON(t *testing.T) {
	body, err := MarshalError("tmux_missing", "tmux is not installed", "retry with --install-tmux")
	if err != nil {
		t.Fatalf("MarshalError returned error: %v", err)
	}
	var got Error
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got.OK || got.Error != "tmux_missing" || got.Hint != "retry with --install-tmux" {
		t.Fatalf("unexpected error response: %#v", got)
	}
}
```

- [ ] **Step 2: Write failing root command test**

Create `internal/cli/root_test.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
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
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/response ./internal/cli
```

Expected: FAIL because the module and packages do not exist yet.

- [ ] **Step 4: Implement module and response package**

Create `go.mod`:

```go
module github.com/agent-ssh/assh

go 1.22

require github.com/spf13/cobra v1.8.1
```

Create `internal/response/response.go`:

```go
package response

import "encoding/json"

type OK map[string]any

type Error struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

func Marshal(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func MarshalError(code, message, hint string) ([]byte, error) {
	return Marshal(Error{OK: false, Error: code, Message: message, Hint: hint})
}
```

- [ ] **Step 5: Implement root command and entrypoint**

Create `internal/cli/root.go`:

```go
package cli

import (
	"errors"
	"fmt"

	"github.com/agent-ssh/assh/internal/response"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "assh-go",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := response.MarshalError("invalid_args", "command required", "run assh-go help")
			_, _ = cmd.OutOrStdout().Write(body)
			return errors.New("command required")
		},
	}
	cmd.PersistentFlags().Bool("json", true, "emit JSON output")
	return cmd
}

func Execute() error {
	cmd := NewRootCommand()
	if err := cmd.Execute(); err != nil {
		if cmd.Context() == nil {
			return err
		}
		return fmt.Errorf("%w", err)
	}
	return nil
}
```

Create `cmd/assh-go/main.go`:

```go
package main

import (
	"os"

	"github.com/agent-ssh/assh/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Run tests**

Run:

```bash
go mod tidy
go test ./internal/response ./internal/cli
```

Expected: PASS.

- [ ] **Step 7: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add go.mod go.sum cmd internal && git commit -m "feat: scaffold go cli json contract"
```

Expected in current workspace: command may fail before `git add` because there is no git repository.

## Task 2: IDs, State Directories, and Output Paging

**Files:**
- Create: `internal/ids/ids.go`
- Create: `internal/state/dirs.go`
- Create: `internal/state/output.go`
- Test: `internal/ids/ids_test.go`
- Test: `internal/state/output_test.go`

- [ ] **Step 1: Write failing ID tests**

Create `internal/ids/ids_test.go`:

```go
package ids

import "testing"

func TestNewIDIsValid(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if !Valid(id) {
		t.Fatalf("generated invalid id %q", id)
	}
}

func TestValidRejectsUnsafeInput(t *testing.T) {
	bad := []string{"", "../x", "abc/def", "abc def", "abc;rm", "abc\n"}
	for _, value := range bad {
		if Valid(value) {
			t.Fatalf("Valid accepted %q", value)
		}
	}
}
```

- [ ] **Step 2: Write failing output storage tests**

Create `internal/state/output_test.go`:

```go
package state

import "testing"

func TestOutputStoreWriteAndReadPage(t *testing.T) {
	store := NewOutputStore(t.TempDir())
	id := "abc123ef"
	if err := store.Write(id, []byte("a\nb\nc\n"), []byte("err\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	page, err := store.Read(id, "stdout", 1, 1)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if page.Content != "b\n" || page.TotalLines != 3 || page.HasMore != true {
		t.Fatalf("unexpected page: %#v", page)
	}
}

func TestOutputStoreRejectsBadID(t *testing.T) {
	store := NewOutputStore(t.TempDir())
	if _, err := store.Read("../bad", "stdout", 0, 10); err == nil {
		t.Fatalf("expected bad id error")
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/ids ./internal/state
```

Expected: FAIL because packages do not exist.

- [ ] **Step 4: Implement IDs**

Create `internal/ids/ids.go`:

```go
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

var validID = regexp.MustCompile(`^[a-f0-9]{8,32}$`)

func New() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func Valid(value string) bool {
	return validID.MatchString(value)
}
```

- [ ] **Step 5: Implement state directory helper**

Create `internal/state/dirs.go`:

```go
package state

import (
	"os"
	"path/filepath"
	"runtime"
)

func BaseDir() string {
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "assh")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "assh")
		}
	default:
		if v := os.Getenv("XDG_STATE_HOME"); v != "" {
			return filepath.Join(v, "assh")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "assh")
	}
	return ".assh-state"
}
```

- [ ] **Step 6: Implement output storage**

Create `internal/state/output.go`:

```go
package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/agent-ssh/assh/internal/ids"
)

type OutputPage struct {
	OutputID   string `json:"output_id"`
	Stream     string `json:"stream"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	TotalLines int    `json:"total_lines"`
	HasMore    bool   `json:"has_more"`
	Content    string `json:"content"`
}

type OutputStore struct {
	dir string
}

func NewOutputStore(dir string) *OutputStore {
	return &OutputStore{dir: dir}
}

func (s *OutputStore) Write(id string, stdout, stderr []byte) error {
	if !ids.Valid(id) {
		return errors.New("invalid id")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.dir, id), stdout, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, id+".err"), stderr, 0o600)
}

func (s *OutputStore) Read(id, stream string, offset, limit int) (OutputPage, error) {
	if !ids.Valid(id) {
		return OutputPage{}, errors.New("invalid id")
	}
	if stream != "stdout" && stream != "stderr" {
		return OutputPage{}, errors.New("invalid stream")
	}
	if offset < 0 || limit < 1 {
		return OutputPage{}, errors.New("invalid pagination")
	}
	path := filepath.Join(s.dir, id)
	if stream == "stderr" {
		path += ".err"
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return OutputPage{}, err
	}
	lines := bytes.SplitAfter(body, []byte("\n"))
	if len(lines) == 1 && len(lines[0]) == 0 {
		lines = nil
	}
	total := len(lines)
	end := offset + limit
	if end > total {
		end = total
	}
	content := ""
	if offset < total {
		content = string(bytes.Join(lines[offset:end], nil))
	}
	return OutputPage{OutputID: id, Stream: stream, Offset: offset, Limit: limit, TotalLines: total, HasMore: end < total, Content: content}, nil
}
```

- [ ] **Step 7: Run tests**

Run:

```bash
go test ./internal/ids ./internal/state
```

Expected: PASS.

- [ ] **Step 8: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add internal/ids internal/state && git commit -m "feat: add ids and output state"
```

## Task 3: System SSH Transport

**Files:**
- Create: `internal/transport/ssh.go`
- Test: `internal/transport/ssh_test.go`

- [ ] **Step 1: Write failing transport tests**

Create `internal/transport/ssh_test.go`:

```go
package transport

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSSHCommandBuildsArgvWithoutShell(t *testing.T) {
	c := SSHCommand{Binary: "ssh", Host: "example.com", User: "root", Port: 2222, Identity: "key", HostKeyPolicy: "strict"}
	args := c.Args("echo hello")
	joined := strings.Join(args, "\x00")
	if !strings.Contains(joined, "-p\x002222") {
		t.Fatalf("missing port in args: %#v", args)
	}
	if strings.Contains(joined, "sh -c") {
		t.Fatalf("transport must not use local shell: %#v", args)
	}
}

func TestRunUsesMockSSH(t *testing.T) {
	dir := t.TempDir()
	name := "ssh"
	if runtime.GOOS == "windows" {
		name = "ssh.bat"
	}
	mock := filepath.Join(dir, name)
	script := "#!/bin/sh\necho stdout\n>&2 echo stderr\nexit 7\n"
	if runtime.GOOS == "windows" {
		script = "@echo stdout\r\n@echo stderr 1>&2\r\n@exit /b 7\r\n"
	}
	if err := os.WriteFile(mock, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := SSHCommand{Binary: name, Host: "example.com", User: "root"}
	res := c.Run(context.Background(), "ignored")
	if res.ExitCode != 7 || strings.TrimSpace(string(res.Stdout)) != "stdout" {
		t.Fatalf("unexpected result: %#v", res)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/transport
```

Expected: FAIL because package does not exist.

- [ ] **Step 3: Implement transport**

Create `internal/transport/ssh.go`:

```go
package transport

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
)

type SSHCommand struct {
	Binary        string
	Host          string
	User          string
	Port          int
	Identity      string
	TimeoutSecond int
	HostKeyPolicy string
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

func (c SSHCommand) Args(remoteCommand string) []string {
	args := []string{}
	policy := c.HostKeyPolicy
	if policy == "" {
		policy = "accept-new"
	}
	switch policy {
	case "accept-new":
		args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	case "strict":
		args = append(args, "-o", "StrictHostKeyChecking=yes")
	case "no-check":
		args = append(args, "-o", "StrictHostKeyChecking=no")
	}
	if c.Port > 0 && c.Port != 22 {
		args = append(args, "-p", fmtInt(c.Port))
	}
	if c.Identity != "" {
		args = append(args, "-i", c.Identity)
	}
	target := c.Host
	if c.User != "" {
		target = c.User + "@" + c.Host
	}
	args = append(args, target, remoteCommand)
	return args
}

func (c SSHCommand) Run(ctx context.Context, remoteCommand string) Result {
	bin := c.Binary
	if bin == "" {
		bin = "ssh"
	}
	cmd := exec.CommandContext(ctx, bin, c.Args(remoteCommand)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: code, Err: err}
}

func fmtInt(v int) string {
	return strconv.Itoa(v)
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/transport/ssh.go internal/transport/ssh_test.go
go test ./internal/transport
```

Expected: PASS.

- [ ] **Step 5: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add internal/transport && git commit -m "feat: add system ssh transport"
```

## Task 4: Exec and Read Commands

**Files:**
- Modify: `internal/cli/root.go`
- Create: `internal/cli/exec.go`
- Test: `internal/cli/exec_test.go`

- [ ] **Step 1: Write failing CLI tests**

Create `internal/cli/exec_test.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReadMissingIDReturnsJSONError(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"read"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	var got map[string]any
	if json.Unmarshal(out.Bytes(), &got) != nil {
		t.Fatalf("expected json, got %q", out.String())
	}
	if got["error"] != "invalid_args" {
		t.Fatalf("unexpected response: %#v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/cli
```

Expected: FAIL because `read` command is not registered.

- [ ] **Step 3: Register exec/read commands**

Modify `internal/cli/root.go` so `NewRootCommand` adds commands:

```go
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "assh-go",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := response.MarshalError("invalid_args", "command required", "run assh-go help")
			_, _ = cmd.OutOrStdout().Write(body)
			return errors.New("command required")
		},
	}
	cmd.PersistentFlags().Bool("json", true, "emit JSON output")
	cmd.AddCommand(newExecCommand(), newReadCommand())
	return cmd
}
```

Create `internal/cli/exec.go`:

```go
package cli

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/agent-ssh/assh/internal/ids"
	"github.com/agent-ssh/assh/internal/response"
	"github.com/agent-ssh/assh/internal/state"
	"github.com/agent-ssh/assh/internal/transport"
	"github.com/spf13/cobra"
)

func newExecCommand() *cobra.Command {
	var host, user, identity, hostKeyPolicy string
	var port, timeout int
	cmd := &cobra.Command{
		Use:   "exec -- command",
		Short: "execute remote command and store output",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" || len(args) == 0 {
				body, _ := response.MarshalError("invalid_args", "--host and command are required", "")
				_, _ = cmd.OutOrStdout().Write(body)
				return errors.New("invalid args")
			}
			id, err := ids.New()
			if err != nil {
				body, _ := response.MarshalError("command_failed", err.Error(), "")
				_, _ = cmd.OutOrStdout().Write(body)
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()
			result := transport.SSHCommand{Host: host, User: user, Port: port, Identity: identity, HostKeyPolicy: hostKeyPolicy}.Run(ctx, args[0])
			store := state.NewOutputStore(filepath.Join(state.BaseDir(), "outputs"))
			if err := store.Write(id, result.Stdout, result.Stderr); err != nil {
				body, _ := response.MarshalError("command_failed", err.Error(), "")
				_, _ = cmd.OutOrStdout().Write(body)
				return err
			}
			body, _ := response.Marshal(response.OK{"ok": true, "exit_code": result.ExitCode, "output_id": id, "stdout_lines": countLines(result.Stdout), "stderr_lines": countLines(result.Stderr)})
			_, _ = cmd.OutOrStdout().Write(body)
			return nil
		},
	}
	cmd.Flags().StringVarP(&host, "host", "H", "", "remote host")
	cmd.Flags().StringVarP(&user, "user", "u", "root", "remote user")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "remote port")
	cmd.Flags().StringVarP(&identity, "identity", "i", "", "identity file")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 300, "timeout seconds")
	cmd.Flags().StringVar(&hostKeyPolicy, "host-key-policy", "accept-new", "accept-new|strict|no-check")
	return cmd
}

func newReadCommand() *cobra.Command {
	var id, stream string
	var offset, limit int
	cmd := &cobra.Command{
		Use:   "read",
		Short: "read stored command output",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				body, _ := response.MarshalError("invalid_args", "--id is required", "")
				_, _ = cmd.OutOrStdout().Write(body)
				return errors.New("missing id")
			}
			store := state.NewOutputStore(filepath.Join(state.BaseDir(), "outputs"))
			page, err := store.Read(id, stream, offset, limit)
			if err != nil {
				body, _ := response.MarshalError("output_not_found", err.Error(), "")
				_, _ = cmd.OutOrStdout().Write(body)
				return err
			}
			body, _ := response.Marshal(page)
			_, _ = cmd.OutOrStdout().Write(body)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "output id")
	cmd.Flags().StringVar(&stream, "stream", "stdout", "stdout|stderr")
	cmd.Flags().IntVar(&offset, "offset", 0, "line offset")
	cmd.Flags().IntVar(&limit, "limit", 50, "line limit")
	return cmd
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/cli
go test ./internal/cli ./internal/state ./internal/transport
```

Expected: PASS.

- [ ] **Step 5: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add internal/cli && git commit -m "feat: add exec and read commands"
```

## Task 5: Remote Quoting and Session Metadata

**Files:**
- Create: `internal/remote/shell.go`
- Create: `internal/session/service.go`
- Test: `internal/remote/shell_test.go`
- Test: `internal/session/service_test.go`

- [ ] **Step 1: Write failing quoting tests**

Create `internal/remote/shell_test.go`:

```go
package remote

import "testing"

func TestSingleQuote(t *testing.T) {
	got := SingleQuote("a'b")
	want := "'a'\"'\"'b'"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSafeSID(t *testing.T) {
	if !SafeSID("abcdef12") {
		t.Fatalf("expected valid sid")
	}
	if SafeSID("../bad") {
		t.Fatalf("expected invalid sid")
	}
}
```

- [ ] **Step 2: Write failing session metadata tests**

Create `internal/session/service_test.go`:

```go
package session

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMetadataJSON(t *testing.T) {
	meta := Metadata{CreatedBy: "assh", SID: "abcdef12", Label: "deploy", TmuxName: "assh_abcdef12", TTLSeconds: 3600}
	body, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Metadata
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CreatedBy != "assh" || got.TmuxName != "assh_abcdef12" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
}

func TestExpired(t *testing.T) {
	meta := Metadata{CreatedAt: time.Now().Add(-2 * time.Hour), TTLSeconds: 3600}
	if !meta.Expired(time.Now()) {
		t.Fatalf("expected expired metadata")
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/remote ./internal/session
```

Expected: FAIL because packages do not exist.

- [ ] **Step 4: Implement remote shell helpers**

Create `internal/remote/shell.go`:

```go
package remote

import (
	"regexp"
	"strings"
)

var safeSID = regexp.MustCompile(`^[a-f0-9]{8,32}$`)

func SingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func SafeSID(value string) bool {
	return safeSID.MatchString(value)
}
```

- [ ] **Step 5: Implement session metadata**

Create `internal/session/service.go`:

```go
package session

import "time"

type Metadata struct {
	CreatedBy  string    `json:"created_by"`
	SID        string    `json:"sid"`
	Label      string    `json:"label"`
	TmuxName   string    `json:"tmux_name"`
	CreatedAt  time.Time `json:"created_at"`
	TTLSeconds int64     `json:"ttl_seconds"`
	ClientID   string    `json:"client_id,omitempty"`
}

func NewMetadata(sid, label string, ttl time.Duration, clientID string) Metadata {
	return Metadata{
		CreatedBy:  "assh",
		SID:        sid,
		Label:      label,
		TmuxName:   "assh_" + sid,
		CreatedAt:  time.Now().UTC(),
		TTLSeconds: int64(ttl.Seconds()),
		ClientID:   clientID,
	}
}

func (m Metadata) Expired(now time.Time) bool {
	if m.TTLSeconds <= 0 {
		return false
	}
	return m.CreatedAt.Add(time.Duration(m.TTLSeconds) * time.Second).Before(now)
}
```

- [ ] **Step 6: Run tests**

Run:

```bash
gofmt -w internal/remote internal/session
go test ./internal/remote ./internal/session
```

Expected: PASS.

- [ ] **Step 7: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add internal/remote internal/session && git commit -m "feat: add remote quoting and session metadata"
```

## Task 6: Capabilities and tmux Bootstrap Detection

**Files:**
- Create: `internal/capabilities/service.go`
- Create: `internal/cli/capabilities.go`
- Modify: `internal/cli/root.go`
- Test: `internal/capabilities/service_test.go`
- Test: `internal/cli/capabilities_test.go`

- [ ] **Step 1: Write failing capabilities parser test**

Create `internal/capabilities/service_test.go`:

```go
package capabilities

import "testing"

func TestParseProbe(t *testing.T) {
	raw := "os=linux\ntmux=missing\npkg=apt\ninstall=noninteractive\n"
	got := ParseProbe(raw)
	if got.OS != "linux" || got.TmuxInstalled || got.PackageManager != "apt" || !got.NonInteractiveInstall {
		t.Fatalf("unexpected capabilities: %#v", got)
	}
}
```

- [ ] **Step 2: Implement capabilities service**

Create `internal/capabilities/service.go`:

```go
package capabilities

import "strings"

type Capabilities struct {
	OK                    bool   `json:"ok"`
	OS                    string `json:"os"`
	TmuxInstalled         bool   `json:"tmux_installed"`
	PackageManager        string `json:"package_manager,omitempty"`
	NonInteractiveInstall bool   `json:"non_interactive_install"`
	SessionBackend         string `json:"session_backend"`
}

func ProbeCommand() string {
	return `printf 'os=%s\n' "$(uname -s 2>/dev/null | tr A-Z a-z || echo unknown)"; if command -v tmux >/dev/null 2>&1; then echo tmux=installed; else echo tmux=missing; fi; for p in apt dnf yum apk pacman brew; do if command -v "$p" >/dev/null 2>&1; then echo pkg=$p; break; fi; done; if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then echo install=noninteractive; else echo install=unknown; fi`
}

func ParseProbe(raw string) Capabilities {
	c := Capabilities{OK: true, SessionBackend: "unsupported"}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "os":
			c.OS = value
			if strings.Contains(value, "linux") || strings.Contains(value, "darwin") {
				c.SessionBackend = "tmux"
			}
		case "tmux":
			c.TmuxInstalled = value == "installed"
		case "pkg":
			c.PackageManager = value
		case "install":
			c.NonInteractiveInstall = value == "noninteractive"
		}
	}
	return c
}
```

- [ ] **Step 3: Add CLI command**

Create `internal/cli/capabilities.go`:

```go
package cli

import (
	"context"
	"errors"

	"github.com/agent-ssh/assh/internal/capabilities"
	"github.com/agent-ssh/assh/internal/response"
	"github.com/agent-ssh/assh/internal/transport"
	"github.com/spf13/cobra"
)

func newCapabilitiesCommand() *cobra.Command {
	var host, user, identity string
	var port int
	cmd := &cobra.Command{
		Use: "capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				body, _ := response.MarshalError("invalid_args", "--host is required", "")
				_, _ = cmd.OutOrStdout().Write(body)
				return errors.New("missing host")
			}
			res := transport.SSHCommand{Host: host, User: user, Port: port, Identity: identity}.Run(context.Background(), capabilities.ProbeCommand())
			if res.Err != nil {
				body, _ := response.MarshalError("connection_error", string(res.Stderr), "")
				_, _ = cmd.OutOrStdout().Write(body)
				return res.Err
			}
			body, _ := response.Marshal(capabilities.ParseProbe(string(res.Stdout)))
			_, _ = cmd.OutOrStdout().Write(body)
			return nil
		},
	}
	cmd.Flags().StringVarP(&host, "host", "H", "", "remote host")
	cmd.Flags().StringVarP(&user, "user", "u", "root", "remote user")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "remote port")
	cmd.Flags().StringVarP(&identity, "identity", "i", "", "identity file")
	return cmd
}
```

Modify `internal/cli/root.go`:

```go
cmd.AddCommand(newExecCommand(), newReadCommand(), newCapabilitiesCommand())
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/capabilities internal/cli
go test ./internal/capabilities ./internal/cli
```

Expected: PASS.

- [ ] **Step 5: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add internal/capabilities internal/cli && git commit -m "feat: add capabilities command"
```

## Task 7: Session Commands and Safe Cleanup Guards

**Files:**
- Modify: `internal/session/service.go`
- Create: `internal/cli/session.go`
- Modify: `internal/cli/root.go`
- Test: `internal/session/service_test.go`
- Test: `internal/cli/session_test.go`

- [ ] **Step 1: Add failing cleanup guard test**

Append to `internal/session/service_test.go`:

```go
func TestCanCleanupRequiresAsshMarker(t *testing.T) {
	good := Metadata{CreatedBy: "assh", SID: "abcdef12", TmuxName: "assh_abcdef12"}
	if !CanCleanup(good) {
		t.Fatalf("expected cleanup allowed")
	}
	bad := Metadata{CreatedBy: "other", SID: "abcdef12", TmuxName: "assh_abcdef12"}
	if CanCleanup(bad) {
		t.Fatalf("expected cleanup refused")
	}
}
```

- [ ] **Step 2: Implement cleanup guard and remote command builders**

Append to `internal/session/service.go`:

```go
func CanCleanup(m Metadata) bool {
	return m.CreatedBy == "assh" && m.SID != "" && m.TmuxName == "assh_"+m.SID
}

func OpenRemoteCommand(metaJSON string, tmuxName string) string {
	return "mkdir -p ~/.assh/sessions && " +
		"mkdir -p ~/.assh/sessions/" + tmuxName[len("assh_"):] + " && " +
		"printf %s " + remote.SingleQuote(metaJSON) + " > ~/.assh/sessions/" + tmuxName[len("assh_"):] + "/meta.json && " +
		"tmux new-session -d -s " + remote.SingleQuote(tmuxName)
}
```

Add import:

```go
import (
	"time"

	"github.com/agent-ssh/assh/internal/remote"
)
```

- [ ] **Step 3: Create session CLI commands**

Create `internal/cli/session.go`:

```go
package cli

import (
	"errors"

	"github.com/agent-ssh/assh/internal/response"
	"github.com/spf13/cobra"
)

func newSessionCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "session"}
	cmd.AddCommand(newSessionOpenCommand(), newSessionCloseCommand(), newSessionGCCommand())
	return cmd
}

func newSessionOpenCommand() *cobra.Command {
	var host, label string
	var installTmux bool
	cmd := &cobra.Command{
		Use: "open",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				body, _ := response.MarshalError("invalid_args", "--host is required", "")
				_, _ = cmd.OutOrStdout().Write(body)
				return errors.New("missing host")
			}
			body, _ := response.Marshal(response.OK{"ok": true, "operation": "session_open", "host": host, "label": label, "install_tmux": installTmux})
			_, _ = cmd.OutOrStdout().Write(body)
			return nil
		},
	}
	cmd.Flags().StringVarP(&host, "host", "H", "", "remote host")
	cmd.Flags().StringVarP(&label, "name", "n", "", "session label")
	cmd.Flags().BoolVar(&installTmux, "install-tmux", false, "install tmux when missing")
	return cmd
}

func newSessionCloseCommand() *cobra.Command {
	return &cobra.Command{Use: "close", RunE: func(cmd *cobra.Command, args []string) error {
		body, _ := response.MarshalError("invalid_args", "--sid is required", "")
		_, _ = cmd.OutOrStdout().Write(body)
		return errors.New("missing sid")
	}}
}

func newSessionGCCommand() *cobra.Command {
	return &cobra.Command{Use: "gc", RunE: func(cmd *cobra.Command, args []string) error {
		body, _ := response.Marshal(response.OK{"ok": true, "dry_run": true, "candidates": []string{}})
		_, _ = cmd.OutOrStdout().Write(body)
		return nil
	}}
}
```

Modify `internal/cli/root.go`:

```go
cmd.AddCommand(newExecCommand(), newReadCommand(), newCapabilitiesCommand(), newSessionCommand())
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/session internal/cli
go test ./internal/session ./internal/cli
```

Expected: PASS.

- [ ] **Step 5: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add internal/session internal/cli && git commit -m "feat: add session lifecycle guards"
```

## Task 8: Session Remote Operations

**Files:**
- Modify: `internal/session/service.go`
- Modify: `internal/cli/session.go`
- Test: `internal/session/service_test.go`
- Test: `internal/cli/session_test.go`

- [ ] **Step 1: Add failing tests for remote command builders**

Append to `internal/session/service_test.go`:

```go
func TestExecRemoteCommandWritesSeqFiles(t *testing.T) {
	got := ExecRemoteCommand("abcdef12", "assh_abcdef12", 3, "pwd")
	for _, want := range []string{"~/.assh/sessions/abcdef12/3.out", "~/.assh/sessions/abcdef12/3.err", "~/.assh/sessions/abcdef12/3.rc", "tmux send-keys"} {
		if !strings.Contains(got, want) {
			t.Fatalf("command %q missing %q", got, want)
		}
	}
}

func TestCloseRemoteCommandChecksMarker(t *testing.T) {
	got := CloseRemoteCommand("abcdef12", "assh_abcdef12")
	for _, want := range []string{"created_by", "assh", "tmux kill-session", "rm -rf ~/.assh/sessions/abcdef12"} {
		if !strings.Contains(got, want) {
			t.Fatalf("command %q missing %q", got, want)
		}
	}
}
```

Add `strings` to the test imports:

```go
import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)
```

- [ ] **Step 2: Implement session remote command builders**

Append to `internal/session/service.go`:

```go
func ExecRemoteCommand(sid, tmuxName string, seq int, command string) string {
	dir := "~/.assh/sessions/" + sid
	out := dir + "/" + strconv.Itoa(seq) + ".out"
	errPath := dir + "/" + strconv.Itoa(seq) + ".err"
	rc := dir + "/" + strconv.Itoa(seq) + ".rc"
	wrapped := command + " > " + out + " 2> " + errPath + "; echo $? > " + rc
	return "mkdir -p " + dir + " && tmux send-keys -t " + remote.SingleQuote(tmuxName) + " " + remote.SingleQuote(wrapped) + " Enter"
}

func ReadRemoteCommand(sid string, seq int, stream string, offset int, limit int) string {
	ext := "out"
	if stream == "stderr" {
		ext = "err"
	}
	file := "~/.assh/sessions/" + sid + "/" + strconv.Itoa(seq) + "." + ext
	return "test -f " + file + " || { echo __ASSH_NOT_FOUND__; exit 0; }; " +
		"total=$(wc -l < " + file + "); " +
		"tail -n +" + strconv.Itoa(offset+1) + " " + file + " | head -n " + strconv.Itoa(limit) + "; " +
		"printf '\\n__ASSH_TOTAL_LINES__=%s\\n' \"$total\""
}

func CloseRemoteCommand(sid, tmuxName string) string {
	dir := "~/.assh/sessions/" + sid
	return "test -f " + dir + "/meta.json || exit 0; " +
		"grep -q '\"created_by\":\"assh\"' " + dir + "/meta.json || exit 3; " +
		"tmux kill-session -t " + remote.SingleQuote(tmuxName) + " 2>/dev/null || true; " +
		"rm -rf " + dir
}
```

Add `strconv` to `internal/session/service.go` imports.

- [ ] **Step 3: Extend session CLI with required subcommands**

Replace `newSessionCommand` in `internal/cli/session.go` with:

```go
func newSessionCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "session"}
	cmd.AddCommand(
		newSessionOpenCommand(),
		newSessionExecCommand(),
		newSessionReadCommand(),
		newSessionCloseCommand(),
		newSessionGCCommand(),
	)
	return cmd
}
```

Append these command constructors to `internal/cli/session.go`:

```go
func newSessionExecCommand() *cobra.Command {
	var sid string
	cmd := &cobra.Command{
		Use: "exec -- command",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sid == "" || len(args) == 0 {
				body, _ := response.MarshalError("invalid_args", "--sid and command are required", "")
				_, _ = cmd.OutOrStdout().Write(body)
				return errors.New("missing sid or command")
			}
			body, _ := response.Marshal(response.OK{"ok": true, "operation": "session_exec", "sid": sid})
			_, _ = cmd.OutOrStdout().Write(body)
			return nil
		},
	}
	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	return cmd
}

func newSessionReadCommand() *cobra.Command {
	var sid string
	var seq int
	cmd := &cobra.Command{
		Use: "read",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sid == "" || seq < 1 {
				body, _ := response.MarshalError("invalid_args", "--sid and --seq are required", "")
				_, _ = cmd.OutOrStdout().Write(body)
				return errors.New("missing sid or seq")
			}
			body, _ := response.Marshal(response.OK{"ok": true, "operation": "session_read", "sid": sid, "seq": seq})
			_, _ = cmd.OutOrStdout().Write(body)
			return nil
		},
	}
	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().IntVar(&seq, "seq", 0, "session command sequence")
	return cmd
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/session internal/cli
go test ./internal/session ./internal/cli
```

Expected: PASS.

- [ ] **Step 5: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add internal/session internal/cli && git commit -m "feat: add session remote operations"
```

## Task 9: Audit and Documentation Update

**Files:**
- Create: `internal/audit/audit.go`
- Test: `internal/audit/audit_test.go`
- Modify: `README.md`
- Modify: `AGENT_INSTRUCTIONS.md`
- Modify: `SYSTEM_PROMPT_snippet.md`

- [ ] **Step 1: Write failing audit test**

Create `internal/audit/audit_test.go`:

```go
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOmitsCommandText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	err := Write(path, Event{Action: "exec", Host: "h", User: "root", CommandHash: "abc"})
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid jsonl entry: %v", err)
	}
	if _, exists := got["command"]; exists {
		t.Fatalf("audit must not include raw command: %#v", got)
	}
}
```

- [ ] **Step 2: Implement audit writer**

Create `internal/audit/audit.go`:

```go
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Event struct {
	Timestamp   time.Time `json:"ts"`
	Action      string    `json:"action"`
	Host        string    `json:"host,omitempty"`
	User        string    `json:"user,omitempty"`
	CommandHash string    `json:"command_hash,omitempty"`
	ExitCode    int       `json:"exit_code,omitempty"`
	StdoutLines int       `json:"stdout_lines,omitempty"`
	StderrLines int       `json:"stderr_lines,omitempty"`
}

func Write(path string, event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = f.Write(append(body, '\n'))
	return err
}
```

- [ ] **Step 3: Run tests**

Run:

```bash
gofmt -w internal/audit
go test ./internal/audit
```

Expected: PASS.

- [ ] **Step 4: Update docs with v2 direction**

Modify `README.md`, `AGENT_INSTRUCTIONS.md`, and `SYSTEM_PROMPT_snippet.md` to state:

```markdown
Note: v2 targets a Go binary with JSON output by default, system OpenSSH transport, and safe `tmux` session lifecycle. During development, the existing Bash `assh` remains the reference implementation.
```

- [ ] **Step 5: Commit if inside git**

Run:

```bash
git rev-parse --is-inside-work-tree && git add internal/audit README.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md && git commit -m "docs: document go cli migration"
```

## Task 10: Final Verification

**Files:**
- Modify only files needed to fix verification failures.

- [ ] **Step 1: Run full Go test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run formatting**

Run:

```bash
gofmt -w cmd internal
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Build binary**

Run:

```bash
go build -o ./bin/assh-go ./cmd/assh-go
./bin/assh-go --help
```

Expected: build succeeds and help text prints.

- [ ] **Step 4: Cross-compile smoke check**

Run:

```bash
GOOS=linux GOARCH=amd64 go build -o ./bin/assh-go-linux-amd64 ./cmd/assh-go
GOOS=darwin GOARCH=arm64 go build -o ./bin/assh-go-darwin-arm64 ./cmd/assh-go
GOOS=windows GOARCH=amd64 go build -o ./bin/assh-go-windows-amd64.exe ./cmd/assh-go
```

Expected: all builds succeed.

- [ ] **Step 5: Record git status or non-git state**

Run:

```bash
git status --short 2>/dev/null || find . -maxdepth 3 -type f | sort
```

Expected: either clean git state after commits, or a clear file list in the non-git workspace.
