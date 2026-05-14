# assh Agent Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `assh connect` as the v1 first-contact workflow for agents: key bootstrap, capability probe, tmux preparation, safe stale-session cleanup, and session open.

**Architecture:** Add a focused `internal/bootstrap` orchestration package with injected fakes for tests, then expose it through a thin Cobra command in `internal/cli/connect.go`. Keep existing low-level commands (`key-deploy`, `capabilities`, `session`) as reusable primitives and preserve their current behavior.

**Tech Stack:** Go 1.22, Cobra CLI, existing `transport.SSHCommand`, existing `session` registry/remote command helpers, existing `capabilities` probe parser, existing npm and GoReleaser packaging.

---

## File Structure

- Create `internal/bootstrap/service.go`: owns the `connect` workflow, request/result types, injectable dependencies, key auth check, password key deployment, capability probe, tmux install, GC, and session open.
- Create `internal/bootstrap/service_test.go`: unit tests for the workflow with fake SSH, fake key generation, fake password deployment, and isolated state directories.
- Create `internal/cli/connect.go`: Cobra command, flag validation, env password reading policy, JSON response mapping, and help examples.
- Modify `internal/cli/root.go`: register `connect` before lower-level commands so help exposes it as the primary entry point.
- Modify `internal/cli/misc.go`: keep `key-deploy`, expose small reusable helpers only if the CLI command needs them; avoid changing its external flags.
- Modify `internal/cli/session.go`: keep existing session commands; extract only tiny shared functions if needed by `bootstrap`.
- Modify `internal/session/service.go`: add helper functions only when they reduce duplication for registry filtering or next command generation.
- Modify `internal/cli/root_test.go`, `internal/cli/misc_test.go`, and new CLI tests in `internal/cli/connect_test.go`: command registration, validation, password env policy, and JSON shape.
- Delete `assh.bash`: Go binary is the only supported implementation for v1.
- Modify `README.md`, `README.ru.md`, `AGENT_INSTRUCTIONS.md`, and `SYSTEM_PROMPT_snippet.md`: make `assh connect` the primary workflow, document security and limitations, remove Bash MVP references.
- Modify release docs only if they still describe the old Bash path: `docs/superpowers/specs/2026-05-14-assh-v1-release-design.md`, `docs/superpowers/plans/2026-05-14-assh-v1-release.md`.

## Shared Contracts

Use these names consistently across tasks.

```go
package bootstrap

type Request struct {
	Host          string
	User          string
	Port          int
	Identity      string
	PasswordEnv   string
	SessionName   string
	TTL           time.Duration
	Timeout       time.Duration
	HostKeyPolicy string
	GCOlderThan   time.Duration
	SkipGC        bool
	SkipTmuxInstall bool
	StateDir      string
}

type Result struct {
	OK            bool              `json:"ok"`
	Host          string            `json:"host"`
	User          string            `json:"user"`
	Identity      string            `json:"identity"`
	KeyDeployed   bool              `json:"key_deployed"`
	KeyVerified   bool              `json:"key_verified"`
	TmuxInstalled bool              `json:"tmux_installed"`
	GCDeleted     []string          `json:"gc_deleted"`
	GCErrors      []GCError         `json:"gc_errors,omitempty"`
	SID           string            `json:"sid"`
	Session       string            `json:"session"`
	TmuxName      string            `json:"tmux_name"`
	NextCommands  map[string]string `json:"next_commands"`
}

type GCError struct {
	SID   string `json:"sid"`
	Error string `json:"error"`
}

type Error struct {
	Code    string
	Message string
	Hint    string
}

func (e Error) Error() string { return e.Message }
```

Error codes used by `bootstrap.Error.Code`: `invalid_args`, `ssh_missing`, `auth_failed`, `host_key_failed`, `connection_error`, `timeout`, `key_deploy_failed`, `tmux_missing`, `tmux_install_failed`, `command_failed`, `internal_error`.

## Task 1: Bootstrap Service Skeleton And Validation

**Files:**
- Create: `internal/bootstrap/service.go`
- Create: `internal/bootstrap/service_test.go`

- [ ] **Step 1: Write failing validation tests**

Add `internal/bootstrap/service_test.go`:

```go
package bootstrap

import (
	"context"
	"testing"
	"time"
)

func TestRunValidatesRequiredFields(t *testing.T) {
	service := Service{}
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{name: "host", req: Request{User: "root", Port: 22, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "port low", req: Request{Host: "example.com", User: "root", Port: 0, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "ttl", req: Request{Host: "example.com", User: "root", Port: 22, Identity: "/tmp/id", TTL: 0, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "policy", req: Request{Host: "example.com", User: "root", Port: 22, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "bad", StateDir: t.TempDir()}, want: "invalid_args"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Run(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			bootErr, ok := err.(Error)
			if !ok {
				t.Fatalf("expected bootstrap.Error, got %T", err)
			}
			if bootErr.Code != tt.want {
				t.Fatalf("code=%q want %q", bootErr.Code, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bootstrap`

Expected: FAIL because package `internal/bootstrap` or types are missing.

- [ ] **Step 3: Add minimal service types and validation**

Create `internal/bootstrap/service.go` with the shared contracts and:

```go
package bootstrap

import (
	"context"
	"time"
)

type SSHRunner func(context.Context, SSHTarget, string) SSHResult
type KeyEnsurer func(string) error
type PasswordDeployer func(context.Context, string, SSHTarget, string) error
type IDGenerator func() (string, error)

type SSHTarget struct {
	Host          string
	User          string
	Port          int
	Identity      string
	TimeoutSecond int
	HostKeyPolicy string
}

type SSHResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

type Service struct {
	RunSSH         SSHRunner
	EnsureKeyPair  KeyEnsurer
	DeployPassword PasswordDeployer
	NewID          IDGenerator
}

func (s Service) Run(ctx context.Context, req Request) (Result, error) {
	if err := validate(req); err != nil {
		return Result{}, err
	}
	return Result{}, Error{Code: "internal_error", Message: "bootstrap dependencies are not configured"}
}

func validate(req Request) error {
	if req.Host == "" {
		return Error{Code: "invalid_args", Message: "host required"}
	}
	if req.Port < 1 || req.Port > 65535 {
		return Error{Code: "invalid_args", Message: "port must be between 1 and 65535"}
	}
	if req.Timeout <= 0 {
		return Error{Code: "invalid_args", Message: "timeout must be greater than 0"}
	}
	if req.TTL <= 0 {
		return Error{Code: "invalid_args", Message: "ttl must be greater than 0"}
	}
	switch req.HostKeyPolicy {
	case "accept-new", "strict", "no-check":
		return nil
	default:
		return Error{Code: "invalid_args", Message: "invalid host key policy"}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bootstrap`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/service.go internal/bootstrap/service_test.go
git commit -m "feat: add bootstrap service contract"
```

## Task 2: Key Login And Password-To-Key Decision Flow

**Files:**
- Modify: `internal/bootstrap/service.go`
- Modify: `internal/bootstrap/service_test.go`

- [ ] **Step 1: Write failing key-flow tests**

Append to `internal/bootstrap/service_test.go`:

```go
func validRequest(t *testing.T) Request {
	return Request{
		Host:          "10.0.0.1",
		User:          "root",
		Port:          22,
		Identity:      t.TempDir() + "/id_agent_ed25519",
		SessionName:   "deploy",
		TTL:           12 * time.Hour,
		Timeout:       time.Minute,
		HostKeyPolicy: "accept-new",
		GCOlderThan:   24 * time.Hour,
		StateDir:      t.TempDir(),
	}
}

func TestRunDoesNotReadPasswordWhenKeyLoginWorks(t *testing.T) {
	req := validRequest(t)
	req.PasswordEnv = "TARGET_PASS"
	deployCalled := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			if command == keyCheckCommand {
				return SSHResult{ExitCode: 0}
			}
			return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
		},
		DeployPassword: func(context.Context, string, SSHTarget, string) error {
			deployCalled = true
			return nil
		},
		NewID: func() (string, error) { return "abc12345", nil },
	}
	_, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if deployCalled {
		t.Fatal("password deployer was called even though key login worked")
	}
}

func TestRunReturnsAuthFailedWhenKeyLoginFailsWithoutPasswordEnv(t *testing.T) {
	req := validRequest(t)
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		RunSSH: func(context.Context, SSHTarget, string) SSHResult {
			return SSHResult{ExitCode: 255, Err: errFakeSSH, Stderr: []byte("Permission denied")}
		},
	}
	_, err := service.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected auth_failed")
	}
	bootErr := err.(Error)
	if bootErr.Code != "auth_failed" {
		t.Fatalf("code=%q want auth_failed", bootErr.Code)
	}
}

func TestRunDeploysAndVerifiesKeyWhenPasswordEnvIsProvided(t *testing.T) {
	req := validRequest(t)
	req.PasswordEnv = "TARGET_PASS"
	sshCalls := 0
	deployCalls := 0
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			sshCalls++
			if command == keyCheckCommand && sshCalls == 1 {
				return SSHResult{ExitCode: 255, Err: errFakeSSH, Stderr: []byte("Permission denied")}
			}
			return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
		},
		DeployPassword: func(_ context.Context, password string, _ SSHTarget, _ string) error {
			deployCalls++
			if password != "secret" {
				t.Fatalf("password=%q want secret", password)
			}
			return nil
		},
		LookupEnv: func(name string) (string, bool) {
			if name == "TARGET_PASS" {
				return "secret", true
			}
			return "", false
		},
		NewID: func() (string, error) { return "abc12345", nil },
	}
	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if deployCalls != 1 {
		t.Fatalf("deployCalls=%d want 1", deployCalls)
	}
	if !result.KeyDeployed || !result.KeyVerified {
		t.Fatalf("key flags = deployed:%v verified:%v", result.KeyDeployed, result.KeyVerified)
	}
}
```

Add near imports:

```go
var errFakeSSH = errors.New("ssh failed")
```

Update the test imports to include `errors`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/bootstrap`

Expected: FAIL because `LookupEnv`, `keyCheckCommand`, and workflow behavior are missing.

- [ ] **Step 3: Implement key decision flow**

Update `internal/bootstrap/service.go`:

```go
const keyCheckCommand = "true"

type EnvLookup func(string) (string, bool)

type Service struct {
	RunSSH         SSHRunner
	EnsureKeyPair  KeyEnsurer
	DeployPassword PasswordDeployer
	LookupEnv      EnvLookup
	NewID          IDGenerator
}

func (s Service) Run(ctx context.Context, req Request) (Result, error) {
	if err := validate(req); err != nil {
		return Result{}, err
	}
	if s.EnsureKeyPair == nil || s.RunSSH == nil || s.NewID == nil {
		return Result{}, Error{Code: "internal_error", Message: "bootstrap dependencies are not configured"}
	}
	if err := s.EnsureKeyPair(req.Identity); err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}
	target := SSHTarget{Host: req.Host, User: req.User, Port: req.Port, Identity: req.Identity, TimeoutSecond: int(req.Timeout.Seconds()), HostKeyPolicy: req.HostKeyPolicy}
	keyResult := s.RunSSH(ctx, target, keyCheckCommand)
	keyDeployed := false
	if code := sshErrorCode(ctx.Err(), keyResult); code != "" {
		if code != "auth_failed" {
			return Result{}, Error{Code: code, Message: sshErrorMessage(ctx.Err(), keyResult)}
		}
		if req.PasswordEnv == "" {
			return Result{}, Error{Code: "auth_failed", Message: "key login failed and no password env was provided", Hint: "retry with -E PASSWORD_ENV or configure SSH keys"}
		}
		lookup := s.LookupEnv
		if lookup == nil {
			return Result{}, Error{Code: "internal_error", Message: "environment lookup is not configured"}
		}
		password, ok := lookup(req.PasswordEnv)
		if !ok || password == "" {
			return Result{}, Error{Code: "auth_failed", Message: "password env is empty", Hint: "set " + req.PasswordEnv + " before running connect"}
		}
		if s.DeployPassword == nil {
			return Result{}, Error{Code: "internal_error", Message: "password deployer is not configured"}
		}
		if err := s.DeployPassword(ctx, password, target, req.Identity); err != nil {
			return Result{}, Error{Code: "key_deploy_failed", Message: err.Error()}
		}
		keyDeployed = true
		verify := s.RunSSH(ctx, target, keyCheckCommand)
		if code := sshErrorCode(ctx.Err(), verify); code != "" {
			return Result{}, Error{Code: "key_deploy_failed", Message: "key deployment completed but key login verification failed"}
		}
	}
	return s.finishAfterAuth(ctx, req, target, keyDeployed)
}
```

Add `finishAfterAuth` as a temporary method returning a valid minimal result so tests can pass before tmux/session tasks:

```go
func (s Service) finishAfterAuth(ctx context.Context, req Request, target SSHTarget, keyDeployed bool) (Result, error) {
	sid, err := s.NewID()
	if err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}
	return Result{
		OK:          true,
		Host:        req.Host,
		User:        req.User,
		Identity:    req.Identity,
		KeyDeployed: keyDeployed,
		KeyVerified: true,
		SID:         sid,
		Session:     req.SessionName,
		TmuxName:    "assh_" + sid,
	}, nil
}
```

Add `sshErrorCode` and `sshErrorMessage` mirroring the existing CLI classification for `SSHResult`:

```go
func sshErrorCode(ctxErr error, result SSHResult) string {
	if ctxErr != nil {
		return "timeout"
	}
	if result.Err == nil && result.ExitCode == 0 {
		return ""
	}
	text := strings.ToLower(string(result.Stderr) + "\n" + string(result.Stdout) + "\n" + result.Err.Error())
	switch {
	case strings.Contains(text, "permission denied"), strings.Contains(text, "authentication failed"):
		return "auth_failed"
	case strings.Contains(text, "host key verification failed"), strings.Contains(text, "remote host identification has changed"):
		return "host_key_failed"
	case strings.Contains(text, "tmux_missing"):
		return "tmux_missing"
	case strings.Contains(text, "tmux_install_failed"):
		return "tmux_install_failed"
	case result.ExitCode == 127:
		return "ssh_missing"
	case result.ExitCode != 0:
		return "connection_error"
	default:
		return "connection_error"
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/bootstrap`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/service.go internal/bootstrap/service_test.go
git commit -m "feat: add key bootstrap flow"
```

## Task 3: Capability Probe, tmux Preparation, Session Open, And Next Commands

**Files:**
- Modify: `internal/bootstrap/service.go`
- Modify: `internal/bootstrap/service_test.go`
- Modify if needed: `internal/session/service.go`

- [ ] **Step 1: Write failing tmux/session tests**

Append tests:

```go
func TestRunInstallsTmuxWhenProbeReportsMissing(t *testing.T) {
	req := validRequest(t)
	commands := []string{}
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID: func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			commands = append(commands, command)
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=missing\npkg=apt\ninstall=noninteractive\n")}
			case command == installTmuxRemoteCommand:
				return SSHResult{ExitCode: 0}
			case strings.Contains(command, "tmux new-session"):
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}
	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.TmuxInstalled {
		t.Fatal("TmuxInstalled=false want true")
	}
	if result.NextCommands["exec"] != `assh session exec -s abc12345 -- "pwd"` {
		t.Fatalf("next exec=%q", result.NextCommands["exec"])
	}
}

func TestRunReturnsTmuxMissingWhenInstallDisabled(t *testing.T) {
	req := validRequest(t)
	req.SkipTmuxInstall = true
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID: func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			if command == keyCheckCommand {
				return SSHResult{ExitCode: 0}
			}
			if command == probeCommand {
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=missing\npkg=apt\ninstall=noninteractive\n")}
			}
			t.Fatalf("unexpected command: %s", command)
			return SSHResult{}
		},
	}
	_, err := service.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected tmux_missing")
	}
	if err.(Error).Code != "tmux_missing" {
		t.Fatalf("code=%q want tmux_missing", err.(Error).Code)
	}
}
```

Update the test imports to include `strings`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/bootstrap`

Expected: FAIL because `probeCommand`, `installTmuxRemoteCommand`, real session open, and `NextCommands` are missing.

- [ ] **Step 3: Implement probe, tmux install, session open, and registry save**

Update imports in `internal/bootstrap/service.go` to include:

```go
import (
	"encoding/json"
	"strings"

	"github.com/agent-ssh/assh/internal/capabilities"
	"github.com/agent-ssh/assh/internal/session"
)
```

Add constants:

```go
var probeCommand = capabilities.ProbeCommand()
const installTmuxRemoteCommand = "if command -v apt >/dev/null 2>&1; then sudo -n apt update >/dev/null 2>&1 && sudo -n apt install -y tmux || { echo tmux_install_failed >&2; exit 1; }; elif command -v dnf >/dev/null 2>&1; then sudo -n dnf install -y tmux || { echo tmux_install_failed >&2; exit 1; }; elif command -v yum >/dev/null 2>&1; then sudo -n yum install -y tmux || { echo tmux_install_failed >&2; exit 1; }; elif command -v apk >/dev/null 2>&1; then sudo -n apk add tmux || { echo tmux_install_failed >&2; exit 1; }; elif command -v pacman >/dev/null 2>&1; then sudo -n pacman -Sy --noconfirm tmux || { echo tmux_install_failed >&2; exit 1; }; elif command -v brew >/dev/null 2>&1; then brew install tmux || { echo tmux_install_failed >&2; exit 1; }; else echo tmux_missing >&2; exit 127; fi"
```

Replace `finishAfterAuth` with:

```go
func (s Service) finishAfterAuth(ctx context.Context, req Request, target SSHTarget, keyDeployed bool) (Result, error) {
	probe := s.RunSSH(ctx, target, probeCommand)
	if code := sshErrorCode(ctx.Err(), probe); code != "" {
		return Result{}, Error{Code: code, Message: sshErrorMessage(ctx.Err(), probe)}
	}
	caps := capabilities.ParseProbe(probe.Stdout)
	tmuxInstalled := caps.TmuxInstalled
	if caps.SessionBackend != "tmux" {
		return Result{}, Error{Code: "tmux_missing", Message: "remote host does not support tmux sessions"}
	}
	if !caps.TmuxInstalled {
		if req.SkipTmuxInstall {
			return Result{}, Error{Code: "tmux_missing", Message: "tmux is missing and installation is disabled"}
		}
		install := s.RunSSH(ctx, target, installTmuxRemoteCommand)
		if code := sshErrorCode(ctx.Err(), install); code != "" {
			if code == "tmux_missing" || code == "command_failed" || code == "connection_error" {
				return Result{}, Error{Code: "tmux_install_failed", Message: sshErrorMessage(ctx.Err(), install)}
			}
			return Result{}, Error{Code: code, Message: sshErrorMessage(ctx.Err(), install)}
		}
		tmuxInstalled = true
	}
	sid, err := s.NewID()
	if err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}
	metadata := session.NewMetadata(sid, req.SessionName, req.TTL, "")
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}
	openCommand, err := session.OpenRemoteCommand(string(metaJSON), metadata.TmuxName)
	if err != nil {
		return Result{}, Error{Code: "invalid_args", Message: err.Error()}
	}
	open := s.RunSSH(ctx, target, openCommand)
	if code := sshErrorCode(ctx.Err(), open); code != "" {
		return Result{}, Error{Code: code, Message: sshErrorMessage(ctx.Err(), open)}
	}
	entry := session.RegistryEntry{SID: sid, Label: req.SessionName, Host: req.Host, User: req.User, Port: req.Port, Identity: req.Identity, HostKeyPolicy: req.HostKeyPolicy, TmuxName: metadata.TmuxName, CreatedAt: metadata.CreatedAt, TTLSeconds: metadata.TTLSeconds}
	if err := session.SaveRegistry(req.StateDir, entry); err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}
	return Result{
		OK: true, Host: req.Host, User: req.User, Identity: req.Identity,
		KeyDeployed: keyDeployed, KeyVerified: true, TmuxInstalled: tmuxInstalled,
		SID: sid, Session: req.SessionName, TmuxName: metadata.TmuxName,
		NextCommands: map[string]string{
			"exec":  `assh session exec -s ` + sid + ` -- "pwd"`,
			"read":  `assh session read -s ` + sid + ` --seq 1 --limit 50`,
			"close": `assh session close -s ` + sid,
		},
	}, nil
}
```

Add:

```go
func sshErrorMessage(ctxErr error, result SSHResult) string {
	if ctxErr != nil {
		return ctxErr.Error()
	}
	text := strings.TrimSpace(string(result.Stderr))
	if text != "" {
		return text
	}
	text = strings.TrimSpace(string(result.Stdout))
	if text != "" {
		return text
	}
	if result.Err != nil {
		return result.Err.Error()
	}
	return "ssh command failed"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/bootstrap`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/service.go internal/bootstrap/service_test.go internal/session/service.go
git commit -m "feat: open bootstrap tmux session"
```

## Task 4: Safe Bootstrap GC

**Files:**
- Modify: `internal/bootstrap/service.go`
- Modify: `internal/bootstrap/service_test.go`
- Modify if helpful: `internal/session/service.go`

- [ ] **Step 1: Write failing GC tests**

Append:

```go
func TestRunDeletesOnlyOldMatchingRegistryEntriesDuringBootstrapGC(t *testing.T) {
	req := validRequest(t)
	old := session.RegistryEntry{
		SID: "old12345", Label: "old", Host: req.Host, User: req.User, Port: req.Port,
		Identity: req.Identity, HostKeyPolicy: req.HostKeyPolicy, TmuxName: "assh_old12345",
		CreatedAt: time.Now().UTC().Add(-48 * time.Hour), TTLSeconds: int64((12 * time.Hour).Seconds()),
	}
	recent := session.RegistryEntry{
		SID: "new12345", Label: "new", Host: req.Host, User: req.User, Port: req.Port,
		Identity: req.Identity, HostKeyPolicy: req.HostKeyPolicy, TmuxName: "assh_new12345",
		CreatedAt: time.Now().UTC(), TTLSeconds: int64((12 * time.Hour).Seconds()),
	}
	if err := session.SaveRegistry(req.StateDir, old); err != nil { t.Fatal(err) }
	if err := session.SaveRegistry(req.StateDir, recent); err != nil { t.Fatal(err) }
	gcCalled := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID: func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case strings.Contains(command, "old12345") && strings.Contains(command, "metadata_validation_failed"):
				gcCalled = true
				return SSHResult{ExitCode: 0}
			case strings.Contains(command, "tmux new-session"):
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}
	result, err := service.Run(context.Background(), req)
	if err != nil { t.Fatalf("Run() error = %v", err) }
	if !gcCalled {
		t.Fatal("expected remote GC for old session")
	}
	if len(result.GCDeleted) != 1 || result.GCDeleted[0] != "old12345" {
		t.Fatalf("GCDeleted=%v want [old12345]", result.GCDeleted)
	}
	if _, err := session.LoadRegistry(req.StateDir, "old12345"); err == nil {
		t.Fatal("old registry entry still exists")
	}
	if _, err := session.LoadRegistry(req.StateDir, "new12345"); err != nil {
		t.Fatalf("recent registry entry missing: %v", err)
	}
}
```

Update the test imports to include `github.com/agent-ssh/assh/internal/session`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bootstrap`

Expected: FAIL because bootstrap GC is not implemented.

- [ ] **Step 3: Implement GC before opening the new session**

In `finishAfterAuth`, after tmux preparation and before `s.NewID()`, call:

```go
deleted, gcErrors, err := s.runGC(ctx, req, target)
if err != nil {
	return Result{}, err
}
```

Set result fields:

```go
GCDeleted: deleted,
GCErrors: gcErrors,
```

Add helper:

```go
func (s Service) runGC(ctx context.Context, req Request, target SSHTarget) ([]string, []GCError, error) {
	if req.SkipGC {
		return nil, nil, nil
	}
	entries, err := session.ListRegistry(req.StateDir)
	if err != nil {
		return nil, nil, Error{Code: "internal_error", Message: err.Error()}
	}
	now := time.Now().UTC()
	deleted := []string{}
	gcErrors := []GCError{}
	for _, entry := range entries {
		if entry.Host != req.Host || entry.User != req.User || entry.Port != req.Port {
			continue
		}
		if req.GCOlderThan > 0 && entry.CreatedAt.After(now.Add(-req.GCOlderThan)) {
			continue
		}
		remoteCommand, err := session.GCRemoteCommand(entry.SID, entry.TmuxName)
		if err != nil {
			gcErrors = append(gcErrors, GCError{SID: entry.SID, Error: err.Error()})
			continue
		}
		gcTarget := target
		gcTarget.Identity = entry.Identity
		gcTarget.HostKeyPolicy = entry.HostKeyPolicy
		result := s.RunSSH(ctx, gcTarget, remoteCommand)
		if code := sshErrorCode(ctx.Err(), result); code != "" {
			gcErrors = append(gcErrors, GCError{SID: entry.SID, Error: code})
			continue
		}
		if err := session.DeleteRegistry(req.StateDir, entry.SID); err != nil {
			gcErrors = append(gcErrors, GCError{SID: entry.SID, Error: err.Error()})
			continue
		}
		deleted = append(deleted, entry.SID)
	}
	return deleted, gcErrors, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/bootstrap`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/service.go internal/bootstrap/service_test.go
git commit -m "feat: run safe bootstrap session gc"
```

## Task 5: Wire Real CLI Dependencies And `assh connect`

**Files:**
- Create: `internal/cli/connect.go`
- Create: `internal/cli/connect_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/misc.go`

- [ ] **Step 1: Write failing CLI tests**

Create `internal/cli/connect_test.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRootIncludesConnectCommand(t *testing.T) {
	root := NewRootCommand()
	cmd, _, err := root.Find([]string{"connect"})
	if err != nil {
		t.Fatalf("Find(connect) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "connect" {
		t.Fatalf("connect command not registered")
	}
}

func TestConnectRequiresHost(t *testing.T) {
	cmd := NewRootCommand()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"connect"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	var body map[string]any
	if decodeErr := json.Unmarshal(stderr.Bytes(), &body); decodeErr != nil {
		t.Fatalf("stderr is not JSON: %s", stderr.String())
	}
	if body["error"] != "invalid_args" {
		t.Fatalf("error=%v want invalid_args", body["error"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli -run 'TestRootIncludesConnectCommand|TestConnectRequiresHost'`

Expected: FAIL because `connect` is not registered.

- [ ] **Step 3: Implement real dependency adapters and command**

Create `internal/cli/connect.go`:

```go
package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/agent-ssh/assh/internal/bootstrap"
	"github.com/agent-ssh/assh/internal/transport"
	"github.com/spf13/cobra"
)

func newConnectCommand() *cobra.Command {
	var req bootstrap.Request
	var timeoutSeconds int
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Bootstrap SSH access and open an agent tmux session",
		Example: "  export TARGET_PASS='...'\n  assh connect -H 10.0.0.1 -u root -E TARGET_PASS -n deploy\n  unset TARGET_PASS\n\n  assh connect -H 10.0.0.1 -u root -i ~/.ssh/id_agent_ed25519 -n deploy",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Timeout = time.Duration(timeoutSeconds) * time.Second
			if req.Identity == "" {
				req.Identity = filepath.Join(homeDir(), ".ssh", "id_agent_ed25519")
			}
			service := bootstrap.Service{
				RunSSH: func(ctx context.Context, target bootstrap.SSHTarget, remoteCommand string) bootstrap.SSHResult {
					result := runSSH(ctx, transport.SSHCommand{
						Host: target.Host, User: target.User, Port: target.Port, Identity: target.Identity,
						TimeoutSecond: target.TimeoutSecond, HostKeyPolicy: target.HostKeyPolicy,
					}, remoteCommand)
					return bootstrap.SSHResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, Err: result.Err}
				},
				EnsureKeyPair: ensureKeyPair,
				DeployPassword: deployPublicKeyWithPassword,
				LookupEnv: os.LookupEnv,
				NewID: idsNew,
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), req.Timeout)
			defer cancel()
			result, err := service.Run(ctx, req)
			if err != nil {
				var bootErr bootstrap.Error
				if errors.As(err, &bootErr) {
					return writeError(cmd, bootErr.Code, bootErr.Message, bootErr.Hint)
				}
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("connect", req.Host, req.User, "connect", 0, 0, 0)
			return writeJSON(cmd, result)
		},
	}
	cmd.Flags().StringVarP(&req.Host, "host", "H", "", "SSH host")
	cmd.Flags().StringVarP(&req.User, "user", "u", "root", "SSH user")
	cmd.Flags().IntVarP(&req.Port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&req.Identity, "identity", "i", filepath.Join(homeDir(), ".ssh", "id_agent_ed25519"), "identity file")
	cmd.Flags().StringVarP(&req.PasswordEnv, "password-env", "E", "", "password environment variable for first login")
	cmd.Flags().StringVarP(&req.SessionName, "name", "n", "", "session label")
	cmd.Flags().DurationVar(&req.TTL, "ttl", 12*time.Hour, "session ttl")
	cmd.Flags().IntVarP(&timeoutSeconds, "timeout", "t", 300, "timeout in seconds")
	cmd.Flags().StringVar(&req.HostKeyPolicy, "host-key-policy", "accept-new", "host key policy: accept-new, strict, no-check")
	cmd.Flags().DurationVar(&req.GCOlderThan, "gc-older-than", 24*time.Hour, "cleanup sessions older than duration")
	cmd.Flags().BoolVar(&req.SkipGC, "no-gc", false, "skip bootstrap cleanup")
	cmd.Flags().BoolVar(&req.SkipTmuxInstall, "no-install-tmux", false, "do not install tmux if missing")
	return cmd
}
```

Add adapters:

```go
func idsNew() (string, error) {
	return ids.New()
}

func deployPublicKeyWithPassword(ctx context.Context, password string, target bootstrap.SSHTarget, identity string) error {
	pubKey, err := os.ReadFile(identity + ".pub")
	if err != nil {
		return err
	}
	ssh := transport.SSHCommand{Host: target.Host, User: target.User, Port: target.Port, TimeoutSecond: target.TimeoutSecond, HostKeyPolicy: target.HostKeyPolicy}
	return runSSHWithPassword(ctx, password, ssh.Args(keyDeployRemoteCommand(strings.TrimSpace(string(pubKey)))))
}
```

Import `github.com/agent-ssh/assh/internal/ids` and `strings`.

Register command in `internal/cli/root.go` before `newExecCommand()`:

```go
cmd.AddCommand(
	newConnectCommand(),
	newExecCommand(),
	...
)
```

- [ ] **Step 4: Run CLI tests**

Run: `go test ./internal/cli -run 'TestRootIncludesConnectCommand|TestConnectRequiresHost'`

Expected: PASS.

- [ ] **Step 5: Run all Go tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/connect.go internal/cli/connect_test.go internal/cli/root.go internal/cli/misc.go
git commit -m "feat: add connect command"
```

## Task 6: CLI Behavior Coverage And Error Mapping

**Files:**
- Modify: `internal/cli/connect.go`
- Modify: `internal/cli/connect_test.go`
- Modify: `internal/bootstrap/service.go`
- Modify: `internal/bootstrap/service_test.go`

- [ ] **Step 1: Add tests for important CLI and error behavior**

Add tests with dependency seams in `connect.go` if direct execution is hard:

```go
var newBootstrapService = func() bootstrap.Service {
	return bootstrap.Service{...real adapters...}
}
```

Then test:

```go
func TestConnectMapsBootstrapErrorToJSON(t *testing.T) {
	original := newBootstrapService
	defer func() { newBootstrapService = original }()
	newBootstrapService = func() bootstrap.Service {
		return bootstrap.Service{
			EnsureKeyPair: func(string) error { return nil },
			RunSSH: func(context.Context, bootstrap.SSHTarget, string) bootstrap.SSHResult {
				return bootstrap.SSHResult{ExitCode: 255, Err: errors.New("denied"), Stderr: []byte("Permission denied")}
			},
			NewID: func() (string, error) { return "abc12345", nil },
		}
	}
	cmd := NewRootCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"connect", "-H", "10.0.0.1"})
	err := cmd.Execute()
	if err == nil { t.Fatal("expected error") }
	var body map[string]any
	if decodeErr := json.Unmarshal(stderr.Bytes(), &body); decodeErr != nil { t.Fatal(decodeErr) }
	if body["error"] != "auth_failed" { t.Fatalf("error=%v", body["error"]) }
	if strings.Contains(stderr.String(), "secret") { t.Fatal("stderr leaked password") }
}
```

- [ ] **Step 2: Run targeted tests to verify they fail**

Run: `go test ./internal/cli -run TestConnectMapsBootstrapErrorToJSON`

Expected: FAIL until `newBootstrapService` seam and mapping are finished.

- [ ] **Step 3: Finish seams and mapping**

Move real adapter construction into:

```go
var newBootstrapService = func() bootstrap.Service {
	return bootstrap.Service{
		RunSSH: runBootstrapSSH,
		EnsureKeyPair: ensureKeyPair,
		DeployPassword: deployPublicKeyWithPassword,
		LookupEnv: os.LookupEnv,
		NewID: ids.New,
	}
}
```

Use `service := newBootstrapService()` in `newConnectCommand`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli ./internal/bootstrap`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/connect.go internal/cli/connect_test.go internal/bootstrap/service.go internal/bootstrap/service_test.go
git commit -m "test: cover connect error mapping"
```

## Task 7: Remove Bash MVP And Old References

**Files:**
- Delete: `assh.bash`
- Modify: `README.md`
- Modify: `README.ru.md`
- Modify: `AGENT_INSTRUCTIONS.md`
- Modify: `SYSTEM_PROMPT_snippet.md`
- Modify if matching references exist: `docs/superpowers/specs/2026-05-14-assh-v1-release-design.md`
- Modify if matching references exist: `docs/superpowers/plans/2026-05-14-assh-v1-release.md`

- [ ] **Step 1: Find old Bash references**

Run: `rg -n "assh\\.bash|Bash MVP|bash MVP|reference implementation|comparison" .`

Expected: list includes `assh.bash` and any docs references.

- [ ] **Step 2: Delete the old Bash entry point**

Run: `rm assh.bash`

Expected: file is removed from the worktree.

- [ ] **Step 3: Update docs wording**

Replace supported-implementation wording with:

```markdown
`assh` v1 is the Go CLI. The old Bash MVP is no longer shipped as a supported entry point.
```

Remove any install or usage snippets that call `./assh.bash`.

- [ ] **Step 4: Verify references are gone**

Run: `rg -n "assh\\.bash|Bash MVP|bash MVP" .`

Expected: no matches outside historical plan/spec files where the text is explicitly describing removed history. If historical files still match, keep only neutral history statements and do not present Bash as supported.

- [ ] **Step 5: Commit**

```bash
git add -A assh.bash README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md docs/superpowers/specs/2026-05-14-assh-v1-release-design.md docs/superpowers/plans/2026-05-14-assh-v1-release.md
git commit -m "chore: remove bash mvp entrypoint"
```

## Task 8: Documentation For Agent-First v1 Workflow

**Files:**
- Modify: `README.md`
- Modify: `README.ru.md`
- Modify: `AGENT_INSTRUCTIONS.md`
- Modify: `SYSTEM_PROMPT_snippet.md`

- [ ] **Step 1: Update English README opening workflow**

Add near the top of `README.md`:

```markdown
## Quick Start

```bash
npm i -g agent-assh

export TARGET_PASS="..."
assh connect -H 203.0.113.10 -u root -E TARGET_PASS -n deploy
unset TARGET_PASS
```

If key login already works, `assh connect` does not read `TARGET_PASS`.

```bash
assh connect -H 203.0.113.10 -u root -i ~/.ssh/id_agent_ed25519 -n deploy
```
```

Document the returned `next_commands`:

```markdown
After `connect`, continue through the session API:

```bash
assh session exec -s <sid> -- "pwd"
assh session read -s <sid> --seq 1 --limit 50
assh session close -s <sid>
```
```

- [ ] **Step 2: Update Russian README with matching content**

Add near the top of `README.ru.md`:

```markdown
## Быстрый старт

```bash
npm i -g agent-assh

export TARGET_PASS="..."
assh connect -H 203.0.113.10 -u root -E TARGET_PASS -n deploy
unset TARGET_PASS
```

Если вход по ключу уже работает, `assh connect` не читает `TARGET_PASS`.

```bash
assh connect -H 203.0.113.10 -u root -i ~/.ssh/id_agent_ed25519 -n deploy
```
```

- [ ] **Step 3: Update agent instructions**

Replace the first SSH action in `AGENT_INSTRUCTIONS.md` with:

```markdown
1. Start with `assh connect -H <host> -u <user> -E <PASSWORD_ENV> -n <name>` when first-contact password access may be needed.
2. Use the returned `sid` for `assh session exec` and `assh session read`.
3. Prefer `session read --limit 50` and increase the limit only when needed.
4. Close sessions with `assh session close -s <sid>` when finished.
5. Use `assh session gc --execute --older-than 24h` for stale sessions.
```

- [ ] **Step 4: Update system prompt snippet**

Make `SYSTEM_PROMPT_snippet.md` instruct agents to use:

```markdown
Use `assh connect` as the first SSH step. It bootstraps key access, prepares tmux, opens a persistent session, and returns `next_commands`.
```

- [ ] **Step 5: Verify docs contain required sections**

Run: `rg -n "connect|password-env|no-check|session exec|session read|session close|npm i -g agent-assh|GitHub Releases" README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md`

Expected: matches in both languages for install, password-to-key bootstrap, key-only connection, session commands, security notes, and manual GitHub Release install.

- [ ] **Step 6: Commit**

```bash
git add README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
git commit -m "docs: document connect workflow"
```

## Task 9: Release Verification And v1 Readiness

**Files:**
- Modify if needed: `.goreleaser.yaml`
- Modify if needed: `package.json`
- Modify if needed: `scripts/install.js`
- Modify if needed: `scripts/smoke-test.js`

- [ ] **Step 1: Run full verification**

Run:

```bash
gofmt -w internal/bootstrap/service.go internal/bootstrap/service_test.go internal/cli/connect.go internal/cli/connect_test.go internal/cli/root.go internal/cli/misc.go internal/cli/session.go internal/session/service.go
go test ./...
npm test
npm pack --dry-run
```

Expected: all commands pass; `npm pack --dry-run` includes JS installer files and excludes `assh.bash`.

- [ ] **Step 2: Run GoReleaser config check**

Run: `goreleaser check`

Expected: PASS. If it fails only because the local repo has no configured release remote or token, record the exact failure in the final handoff and do not change release code for that reason.

- [ ] **Step 3: Verify help output**

Run:

```bash
go run ./cmd/assh --help
go run ./cmd/assh connect --help
```

Expected: root help lists `connect`; connect help lists `-H`, `-u`, `-p`, `-i`, `-E`, `-n`, `--ttl`, `--timeout`, `--host-key-policy`, `--gc-older-than`, `--no-gc`, and `--no-install-tmux`.

- [ ] **Step 4: Check git state and release version**

Run:

```bash
git status --short
rg -n '"version": "1.0.0"|version = "1.0.0"|v1.0.0|1.0.0' package.json README.md README.ru.md .goreleaser.yaml
```

Expected: git state contains only intended changes before final commit; v1 docs/package metadata still point to `1.0.0`.

- [ ] **Step 5: Commit verification fixes if files changed**

If verification required file changes:

```bash
git add .goreleaser.yaml package.json scripts/install.js scripts/smoke-test.js README.md README.ru.md
git commit -m "chore: finalize v1 release checks"
```

If no files changed, do not create an empty commit.

## Self-Review

- Spec coverage: `assh connect`, password env only, no password argv/prompt, key generation/reuse, deploy and verify key, capabilities probe, tmux install with `--no-install-tmux`, safe GC with `--no-gc`, session open, JSON result with `next_commands`, old Bash removal, bilingual docs, and release checks are each mapped to tasks above.
- Placeholder scan: no task relies on unresolved placeholders; code snippets name concrete files, functions, flags, commands, and expected outputs.
- Type consistency: `Request`, `Result`, `Error`, `SSHRunner`, `SSHTarget`, `SSHResult`, and CLI flags use the same names across service, tests, and CLI wiring.
