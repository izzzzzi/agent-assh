# Session Policy Modes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add CLI-first session policy modes so `assh session exec` can run in `unrestricted`, `readonly`, or `restricted` mode without adding MCP.

**Architecture:** Add an `internal/policy` package that validates policy config and evaluates user command text before the existing dangerous-command safety gate. Persist policy config in `session.RegistryEntry` through `connect` and `session open`, then enforce it in `session exec` before sequence mutation or SSH. Keep existing behavior as the default by treating empty mode as `unrestricted`.

**Tech Stack:** Go 1.22, Cobra CLI, existing JSON response helpers, existing `internal/safety` classifier, Go tests.

---

## File Structure

- Create `internal/policy/policy.go`: policy config validation and command evaluation.
- Create `internal/policy/policy_test.go`: unit coverage for modes, regex handling, readonly denials, and restricted allow/deny behavior.
- Modify `internal/session/service.go`: add policy fields to `RegistryEntry`.
- Modify `internal/bootstrap/service.go`: add policy fields to `Request` and `Result`, validate policy config, save policy fields.
- Modify `internal/bootstrap/service_test.go`: verify policy persistence through bootstrap.
- Modify `internal/cli/session.go`: add `--mode`, `--allow`, `--deny` to `session open`; enforce policy in `session exec`.
- Modify `internal/cli/session_test.go`: CLI coverage for policy storage and denied exec.
- Modify `internal/cli/connect.go`: add policy flags to `connect` and `connect-info`.
- Modify `internal/cli/connect_test.go`: verify connect passes policy into bootstrap.
- Modify `README.md`, `README.en.md`, `AGENT_INSTRUCTIONS.md`, `SYSTEM_PROMPT_snippet.md`, and `internal/cli/prompt.go`: document modes and agent behavior.

## Task 1: Policy Package Tests

**Files:**
- Create: `internal/policy/policy_test.go`

- [ ] **Step 1: Write failing policy tests**

Create `internal/policy/policy_test.go`:

```go
package policy

import "testing"

func TestValidateAcceptsValidConfigs(t *testing.T) {
    tests := []Config{
        {},
        {Mode: ModeUnrestricted},
        {Mode: ModeReadonly},
        {Mode: ModeReadonly, DenyPatterns: []string{`^rm\s`}},
        {Mode: ModeRestricted, AllowPatterns: []string{`^ls\b`}},
        {Mode: ModeRestricted, AllowPatterns: []string{`^journalctl `}, DenyPatterns: []string{`--force`}},
    }
    for _, test := range tests {
        if err := Validate(test); err != nil {
            t.Fatalf("Validate(%#v) error = %v", test, err)
        }
    }
}

func TestValidateRejectsInvalidConfigs(t *testing.T) {
    tests := []struct {
        name string
        cfg  Config
        want string
    }{
        {name: "mode", cfg: Config{Mode: "bad"}, want: "invalid policy mode"},
        {name: "allow on readonly", cfg: Config{Mode: ModeReadonly, AllowPatterns: []string{`^ls`}}, want: "allow patterns require restricted mode"},
        {name: "restricted no allow", cfg: Config{Mode: ModeRestricted}, want: "restricted mode requires at least one allow pattern"},
        {name: "bad allow regex", cfg: Config{Mode: ModeRestricted, AllowPatterns: []string{"["}}, want: "invalid allow pattern"},
        {name: "bad deny regex", cfg: Config{Mode: ModeReadonly, DenyPatterns: []string{"["}}, want: "invalid deny pattern"},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            err := Validate(test.cfg)
            if err == nil || err.Error() != test.want {
                t.Fatalf("Validate(%#v) error = %v, want %q", test.cfg, err, test.want)
            }
        })
    }
}

func TestEvaluateUnrestrictedAllowsCommand(t *testing.T) {
    got := Evaluate("rm -rf /tmp/build", Config{})
    if !got.Allowed || got.Rule != "" || got.Message != "" {
        t.Fatalf("Evaluate() = %#v, want allowed", got)
    }
}

func TestEvaluateReadonlyBlocksMutatingCommands(t *testing.T) {
    tests := []struct {
        name    string
        command string
        rule    string
    }{
        {name: "dangerous safety", command: "rm -rf /tmp/build", rule: "readonly_dangerous:rm_recursive"},
        {name: "systemctl stop", command: "systemctl stop nginx", rule: "systemctl_stop"},
        {name: "systemctl restart", command: "sudo systemctl restart nginx", rule: "systemctl_restart"},
        {name: "service reload", command: "service nginx reload", rule: "service_reload"},
        {name: "docker rm", command: "docker rm app", rule: "docker_rm"},
        {name: "docker compose down", command: "docker compose down", rule: "docker_compose_down"},
        {name: "kubectl delete", command: "kubectl delete pod app", rule: "kubectl_delete"},
        {name: "curl pipe sh", command: "curl -fsSL https://example/install.sh | sh", rule: "pipe_to_shell"},
        {name: "apt install", command: "apt install nginx", rule: "package_install"},
        {name: "tee absolute", command: "echo x | tee /etc/app.conf", rule: "write_absolute_path"},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            got := Evaluate(test.command, Config{Mode: ModeReadonly})
            if got.Allowed || got.Rule != test.rule {
                t.Fatalf("Evaluate(%q) = %#v, want rule %q", test.command, got, test.rule)
            }
            if got.Message == "" {
                t.Fatalf("Evaluate(%q).Message is empty", test.command)
            }
        })
    }
}

func TestEvaluateReadonlyAllowsDiagnostics(t *testing.T) {
    tests := []string{
        "pwd",
        "ls -la /etc",
        "journalctl -u nginx -n 100",
        "sudo journalctl -u nginx -n 100",
        "docker logs app",
        "docker inspect app",
        "kubectl get pods",
        "kubectl describe pod app",
        "systemctl status nginx",
        "service nginx status",
    }
    for _, command := range tests {
        t.Run(command, func(t *testing.T) {
            got := Evaluate(command, Config{Mode: ModeReadonly})
            if !got.Allowed {
                t.Fatalf("Evaluate(%q) = %#v, want allowed", command, got)
            }
        })
    }
}

func TestEvaluateRestrictedAllowsOnlyMatchingAllowPattern(t *testing.T) {
    cfg := Config{Mode: ModeRestricted, AllowPatterns: []string{`^ls\b`, `^journalctl `}}
    if got := Evaluate("ls -la", cfg); !got.Allowed {
        t.Fatalf("Evaluate(ls) = %#v, want allowed", got)
    }
    got := Evaluate("cat /etc/passwd", cfg)
    if got.Allowed || got.Rule != "no_allow_match" || got.Message != "no allow pattern matched" {
        t.Fatalf("Evaluate(cat) = %#v, want no_allow_match", got)
    }
}

func TestEvaluateRestrictedDenyBeatsAllow(t *testing.T) {
    cfg := Config{
        Mode:          ModeRestricted,
        AllowPatterns: []string{`^docker `},
        DenyPatterns:  []string{` rm `, `^docker rm\b`},
    }
    got := Evaluate("docker rm app", cfg)
    if got.Allowed || got.Rule != "deny_pattern" || got.Message != `deny pattern matched: ^docker rm\b` {
        t.Fatalf("Evaluate() = %#v, want deny_pattern", got)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/policy -run 'TestValidate|TestEvaluate' -v
```

Expected: FAIL because `internal/policy` does not exist.

- [ ] **Step 3: Commit red tests is not allowed**

Do not commit yet. Continue to Task 2 and commit tests with implementation once green.

## Task 2: Policy Package Implementation

**Files:**
- Create: `internal/policy/policy.go`
- Test: `internal/policy/policy_test.go`

- [ ] **Step 1: Implement policy package**

Create `internal/policy/policy.go`:

```go
package policy

import (
    "errors"
    "regexp"
    "strings"

    "github.com/izzzzzi/agent-assh/internal/safety"
)

const (
    ModeUnrestricted = "unrestricted"
    ModeReadonly     = "readonly"
    ModeRestricted   = "restricted"
)

type Config struct {
    Mode          string
    AllowPatterns []string
    DenyPatterns  []string
}

type Result struct {
    Allowed bool
    Rule    string
    Message string
}

func Validate(cfg Config) error {
    mode := NormalizeMode(cfg.Mode)
    switch mode {
    case ModeUnrestricted:
        if len(cfg.AllowPatterns) > 0 {
            return errors.New("allow patterns require restricted mode")
        }
        if len(cfg.DenyPatterns) > 0 {
            return errors.New("deny patterns require readonly or restricted mode")
        }
    case ModeReadonly:
        if len(cfg.AllowPatterns) > 0 {
            return errors.New("allow patterns require restricted mode")
        }
    case ModeRestricted:
        if len(cfg.AllowPatterns) == 0 {
            return errors.New("restricted mode requires at least one allow pattern")
        }
    default:
        return errors.New("invalid policy mode")
    }
    if err := validatePatterns("allow", cfg.AllowPatterns); err != nil {
        return err
    }
    return validatePatterns("deny", cfg.DenyPatterns)
}

func NormalizeMode(mode string) string {
    if mode == "" {
        return ModeUnrestricted
    }
    return mode
}

func Evaluate(command string, cfg Config) Result {
    mode := NormalizeMode(cfg.Mode)
    switch mode {
    case ModeReadonly:
        if result := evaluateDenyPatterns(command, cfg.DenyPatterns); !result.Allowed {
            return result
        }
        return evaluateReadonly(command)
    case ModeRestricted:
        if result := evaluateDenyPatterns(command, cfg.DenyPatterns); !result.Allowed {
            return result
        }
        for _, pattern := range cfg.AllowPatterns {
            if regexp.MustCompile(pattern).MatchString(command) {
                return Result{Allowed: true}
            }
        }
        return Result{Allowed: false, Rule: "no_allow_match", Message: "no allow pattern matched"}
    default:
        return Result{Allowed: true}
    }
}

func validatePatterns(label string, patterns []string) error {
    for _, pattern := range patterns {
        if _, err := regexp.Compile(pattern); err != nil {
            return errors.New("invalid " + label + " pattern")
        }
    }
    return nil
}

func evaluateDenyPatterns(command string, patterns []string) Result {
    for _, pattern := range patterns {
        if regexp.MustCompile(pattern).MatchString(command) {
            return Result{Allowed: false, Rule: "deny_pattern", Message: "deny pattern matched: " + pattern}
        }
    }
    return Result{Allowed: true}
}

func evaluateReadonly(command string) Result {
    if result := safety.CheckCommand(command); result.Dangerous {
        return Result{Allowed: false, Rule: "readonly_dangerous:" + result.Rule, Message: result.Message}
    }
    normalized := normalizeCommand(command)
    checks := []struct {
        rule string
        re   string
    }{
        {rule: "systemctl_stop", re: `(^|[;&|]\s*|sudo\s+)systemctl\s+stop\b`},
        {rule: "systemctl_restart", re: `(^|[;&|]\s*|sudo\s+)systemctl\s+restart\b`},
        {rule: "systemctl_reload", re: `(^|[;&|]\s*|sudo\s+)systemctl\s+reload\b`},
        {rule: "systemctl_disable", re: `(^|[;&|]\s*|sudo\s+)systemctl\s+disable\b`},
        {rule: "systemctl_enable", re: `(^|[;&|]\s*|sudo\s+)systemctl\s+enable\b`},
        {rule: "service_stop", re: `(^|[;&|]\s*|sudo\s+)service\s+\S+\s+stop\b`},
        {rule: "service_restart", re: `(^|[;&|]\s*|sudo\s+)service\s+\S+\s+restart\b`},
        {rule: "service_reload", re: `(^|[;&|]\s*|sudo\s+)service\s+\S+\s+reload\b`},
        {rule: "docker_rm", re: `(^|[;&|]\s*|sudo\s+)docker\s+rm\b`},
        {rule: "docker_stop", re: `(^|[;&|]\s*|sudo\s+)docker\s+stop\b`},
        {rule: "docker_restart", re: `(^|[;&|]\s*|sudo\s+)docker\s+restart\b`},
        {rule: "docker_kill", re: `(^|[;&|]\s*|sudo\s+)docker\s+kill\b`},
        {rule: "docker_compose_down", re: `(^|[;&|]\s*|sudo\s+)docker\s+compose\s+down\b`},
        {rule: "kubectl_delete", re: `(^|[;&|]\s*|sudo\s+)kubectl\s+delete\b`},
        {rule: "kubectl_apply", re: `(^|[;&|]\s*|sudo\s+)kubectl\s+apply\b`},
        {rule: "kubectl_replace", re: `(^|[;&|]\s*|sudo\s+)kubectl\s+replace\b`},
        {rule: "kubectl_patch", re: `(^|[;&|]\s*|sudo\s+)kubectl\s+patch\b`},
        {rule: "kubectl_scale", re: `(^|[;&|]\s*|sudo\s+)kubectl\s+scale\b`},
        {rule: "kubectl_rollout_restart", re: `(^|[;&|]\s*|sudo\s+)kubectl\s+rollout\s+restart\b`},
        {rule: "pipe_to_shell", re: `(curl|wget)\b.*\|\s*(sudo\s+)?(sh|bash)\b`},
        {rule: "package_install", re: `(^|[;&|]\s*|sudo\s+)(apt|dnf|yum)\s+(install|remove|update|upgrade)\b`},
        {rule: "package_install", re: `(^|[;&|]\s*|sudo\s+)apk\s+add\b`},
        {rule: "package_install", re: `(^|[;&|]\s*|sudo\s+)pacman\s+-S\b`},
        {rule: "package_install", re: `(^|[;&|]\s*|sudo\s+)brew\s+install\b`},
        {rule: "write_absolute_path", re: `(^|[;&|]\s*|sudo\s+)(tee|cp|mv|install)\b.*\s/(etc|var|usr|bin|sbin|lib|opt|srv|root|home)\b`},
    }
    for _, check := range checks {
        if regexp.MustCompile(check.re).MatchString(normalized) {
            return Result{Allowed: false, Rule: check.rule, Message: "readonly rule matched: " + check.rule}
        }
    }
    if regexp.MustCompile(`(^|[;&|]\s*|sudo\s+)sudo\s+`).MatchString(normalized) && !allowedReadonlySudo(normalized) {
        return Result{Allowed: false, Rule: "sudo", Message: "readonly rule matched: sudo"}
    }
    return Result{Allowed: true}
}

func normalizeCommand(command string) string {
    return strings.Join(strings.Fields(command), " ")
}

func allowedReadonlySudo(command string) bool {
    allowed := []string{
        "sudo journalctl ",
        "sudo tail ",
        "sudo cat ",
        "sudo systemctl status ",
        "sudo systemctl is-active ",
        "sudo docker logs ",
        "sudo docker inspect ",
    }
    for _, prefix := range allowed {
        if strings.HasPrefix(command, prefix) || strings.Contains(command, "; "+prefix) || strings.Contains(command, "| "+prefix) {
            return true
        }
    }
    return false
}
```

- [ ] **Step 2: Run policy tests**

Run:

```bash
go test ./internal/policy -run 'TestValidate|TestEvaluate' -v
```

Expected: PASS.

- [ ] **Step 3: Run gofmt**

Run:

```bash
gofmt -w internal/policy/policy.go internal/policy/policy_test.go
```

Expected: no output.

- [ ] **Step 4: Commit policy package**

Run:

```bash
git add internal/policy/policy.go internal/policy/policy_test.go
git commit -m "Add session policy evaluator"
```

Expected: commit succeeds.

## Task 3: Registry and Bootstrap Persistence

**Files:**
- Modify: `internal/session/service.go`
- Modify: `internal/bootstrap/service.go`
- Modify: `internal/bootstrap/service_test.go`

- [ ] **Step 1: Write failing bootstrap persistence test**

Add this test to `internal/bootstrap/service_test.go` near other successful `Run` tests:

```go
func TestRunPersistsPolicyConfig(t *testing.T) {
    req := validRequest(t)
    req.PolicyMode = "restricted"
    req.AllowPatterns = []string{`^ls\b`}
    req.DenyPatterns = []string{`--force`}
    service := Service{
        EnsureKeyPair: func(string) error { return nil },
        RunSSH: func(_ context.Context, _ SSHTarget, _ string) SSHResult {
            return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
        },
        NewID: func() (string, error) { return "abc12345", nil },
    }

    result, err := service.Run(context.Background(), req)
    if err != nil {
        t.Fatalf("Run() error = %v", err)
    }
    if result.PolicyMode != "restricted" {
        t.Fatalf("result.PolicyMode=%q want restricted", result.PolicyMode)
    }
    entry, err := session.LoadRegistry(req.StateDir, "abc12345")
    if err != nil {
        t.Fatalf("LoadRegistry() error = %v", err)
    }
    if entry.PolicyMode != "restricted" {
        t.Fatalf("entry.PolicyMode=%q want restricted", entry.PolicyMode)
    }
    if !slices.Equal(entry.AllowPatterns, []string{`^ls\b`}) || !slices.Equal(entry.DenyPatterns, []string{`--force`}) {
        t.Fatalf("policy patterns allow=%#v deny=%#v", entry.AllowPatterns, entry.DenyPatterns)
    }
}
```

- [ ] **Step 2: Write failing bootstrap validation test**

Add this case to `TestRunValidatesRequiredFields`:

```go
{
    name: "restricted allow",
    req: Request{
        Host: "example.com", User: "root", Port: 22, Identity: "/tmp/id",
        TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "accept-new",
        StateDir: t.TempDir(), PolicyMode: "restricted",
    },
    want: "invalid_args",
},
```

- [ ] **Step 3: Run bootstrap tests to verify failure**

Run:

```bash
go test ./internal/bootstrap -run 'TestRunPersistsPolicyConfig|TestRunValidatesRequiredFields' -v
```

Expected: FAIL because `Request.PolicyMode`, `Request.AllowPatterns`, `Request.DenyPatterns`, and `Result.PolicyMode` do not exist.

- [ ] **Step 4: Extend session registry model**

In `internal/session/service.go`, update `RegistryEntry`:

```go
type RegistryEntry struct {
    SID           string    `json:"sid"`
    Label         string    `json:"label"`
    Host          string    `json:"host"`
    User          string    `json:"user"`
    Port          int       `json:"port"`
    Identity      string    `json:"identity,omitempty"`
    Jump          string    `json:"jump,omitempty"`
    HostKeyPolicy string    `json:"host_key_policy"`
    TmuxName      string    `json:"tmux_name"`
    CreatedAt     time.Time `json:"created_at"`
    TTLSeconds    int64     `json:"ttl_seconds"`
    Seq           int       `json:"seq"`
    PolicyMode    string    `json:"policy_mode,omitempty"`
    AllowPatterns []string  `json:"allow_patterns,omitempty"`
    DenyPatterns  []string  `json:"deny_patterns,omitempty"`
}
```

- [ ] **Step 5: Extend bootstrap request/result and validation**

In `internal/bootstrap/service.go`, import policy:

```go
import (
    "context"
    "encoding/json"
    "errors"
    "os/exec"
    "strings"
    "time"

    "github.com/izzzzzi/agent-assh/internal/capabilities"
    "github.com/izzzzzi/agent-assh/internal/policy"
    "github.com/izzzzzi/agent-assh/internal/session"
)
```

Add fields to `Request`:

```go
PolicyMode    string
AllowPatterns []string
DenyPatterns  []string
```

Add fields to `Result`:

```go
PolicyMode string `json:"policy_mode,omitempty"`
```

In `validate(req Request) error`, after host key policy validation, add:

```go
if err := policy.Validate(policy.Config{
    Mode:          req.PolicyMode,
    AllowPatterns: req.AllowPatterns,
    DenyPatterns:  req.DenyPatterns,
}); err != nil {
    return Error{Code: "invalid_args", Message: err.Error()}
}
```

In `finishAfterAuth`, set policy fields on the registry entry:

```go
PolicyMode:    policy.NormalizeMode(req.PolicyMode),
AllowPatterns: req.AllowPatterns,
DenyPatterns:  req.DenyPatterns,
```

And include `PolicyMode` in the returned `Result`:

```go
PolicyMode: policy.NormalizeMode(req.PolicyMode),
```

- [ ] **Step 6: Run bootstrap tests**

Run:

```bash
go test ./internal/bootstrap -run 'TestRunPersistsPolicyConfig|TestRunValidatesRequiredFields' -v
```

Expected: PASS.

- [ ] **Step 7: Commit persistence changes**

Run:

```bash
gofmt -w internal/session/service.go internal/bootstrap/service.go internal/bootstrap/service_test.go
git add internal/session/service.go internal/bootstrap/service.go internal/bootstrap/service_test.go
git commit -m "Persist session policy config"
```

Expected: commit succeeds.

## Task 4: Session Open Flags and Session Exec Enforcement

**Files:**
- Modify: `internal/cli/session.go`
- Modify: `internal/cli/session_test.go`

- [ ] **Step 1: Write failing session open policy storage test**

Add this test to `internal/cli/session_test.go` after `TestSessionOpenReturnsPlaceholderJSON`:

```go
func TestSessionOpenStoresPolicyConfig(t *testing.T) {
    setMockSSH(t, "exit 0\n")
    t.Setenv("ASSH_STATE_DIR", t.TempDir())

    got := executeSessionJSON(t, []string{
        "session", "open",
        "--host", "example.com",
        "--mode", "restricted",
        "--allow", `^ls\b`,
        "--deny", `--force`,
    })

    if got["ok"] != true || got["policy_mode"] != "restricted" {
        t.Fatalf("unexpected response: %#v", got)
    }
    entry, err := session.LoadRegistry(stateBaseDir(), got["sid"].(string))
    if err != nil {
        t.Fatalf("LoadRegistry() error = %v", err)
    }
    if entry.PolicyMode != "restricted" || !slices.Equal(entry.AllowPatterns, []string{`^ls\b`}) || !slices.Equal(entry.DenyPatterns, []string{`--force`}) {
        t.Fatalf("entry policy mode=%q allow=%#v deny=%#v", entry.PolicyMode, entry.AllowPatterns, entry.DenyPatterns)
    }
}
```

Add `slices` to the imports:

```go
import (
    "bytes"
    "context"
    "encoding/json"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "slices"
    "strings"
    "testing"
    "time"
)
```

- [ ] **Step 2: Write failing session exec denial tests**

Add these tests near the dangerous command exec tests:

```go
func TestSessionExecPolicyDeniedDoesNotCallSSHOrIncrementSeq(t *testing.T) {
    writeTestSessionRegistry(t, "abcdef12")
    entry, err := session.LoadRegistry(stateBaseDir(), "abcdef12")
    if err != nil {
        t.Fatalf("LoadRegistry() error = %v", err)
    }
    entry.PolicyMode = "readonly"
    if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
        t.Fatalf("SaveRegistry() error = %v", err)
    }
    oldRunSSH := runSSH
    t.Cleanup(func() { runSSH = oldRunSSH })
    runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
        t.Fatalf("runSSH called for policy denied command")
        return transport.Result{}
    }

    got := executeSessionJSONError(t, []string{"session", "exec", "--sid", "abcdef12", "--", "systemctl", "stop", "nginx"})
    if got["error"] != "policy_denied" || got["hint"] != "readonly rule matched: systemctl_stop" {
        t.Fatalf("unexpected response: %#v", got)
    }
    entry, err = session.LoadRegistry(stateBaseDir(), "abcdef12")
    if err != nil {
        t.Fatalf("LoadRegistry() error = %v", err)
    }
    if entry.Seq != 0 {
        t.Fatalf("denied command incremented seq to %d", entry.Seq)
    }
}

func TestSessionExecConfirmDangerDoesNotBypassPolicy(t *testing.T) {
    writeTestSessionRegistry(t, "abcdef12")
    entry, err := session.LoadRegistry(stateBaseDir(), "abcdef12")
    if err != nil {
        t.Fatalf("LoadRegistry() error = %v", err)
    }
    entry.PolicyMode = "readonly"
    if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
        t.Fatalf("SaveRegistry() error = %v", err)
    }

    got := executeSessionJSONError(t, []string{"session", "exec", "--sid", "abcdef12", "--confirm-danger", "--", "docker", "rm", "app"})
    if got["error"] != "policy_denied" || got["hint"] != "readonly rule matched: docker_rm" {
        t.Fatalf("unexpected response: %#v", got)
    }
}

func TestSessionExecRestrictedAllowedStillRequiresDangerConfirmation(t *testing.T) {
    writeTestSessionRegistry(t, "abcdef12")
    entry, err := session.LoadRegistry(stateBaseDir(), "abcdef12")
    if err != nil {
        t.Fatalf("LoadRegistry() error = %v", err)
    }
    entry.PolicyMode = "restricted"
    entry.AllowPatterns = []string{`^rm `}
    if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
        t.Fatalf("SaveRegistry() error = %v", err)
    }

    got := executeSessionJSONError(t, []string{"session", "exec", "--sid", "abcdef12", "--", "rm", "-rf", "/tmp/build"})
    if got["error"] != "dangerous_command_requires_confirmation" {
        t.Fatalf("unexpected response: %#v", got)
    }
}
```

- [ ] **Step 3: Run session tests to verify failure**

Run:

```bash
go test ./internal/cli -run 'TestSessionOpenStoresPolicyConfig|TestSessionExecPolicy|TestSessionExecConfirmDanger|TestSessionExecRestrictedAllowed' -v
```

Expected: FAIL because policy CLI flags and enforcement are not implemented.

- [ ] **Step 4: Add session open policy flags**

In `internal/cli/session.go`, import policy:

```go
import (
    "context"
    "encoding/json"
    "strconv"
    "strings"
    "time"

    "github.com/izzzzzi/agent-assh/internal/ids"
    "github.com/izzzzzi/agent-assh/internal/policy"
    "github.com/izzzzzi/agent-assh/internal/remote"
    "github.com/izzzzzi/agent-assh/internal/response"
    "github.com/izzzzzi/agent-assh/internal/safety"
    "github.com/izzzzzi/agent-assh/internal/session"
    "github.com/izzzzzi/agent-assh/internal/state"
    "github.com/izzzzzi/agent-assh/internal/transport"
    "github.com/spf13/cobra"
)
```

In `newSessionOpenCommand`, add local variables:

```go
var mode string
var allowPatterns []string
var denyPatterns []string
```

Before creating `sid`, validate policy:

```go
policyConfig := policy.Config{Mode: mode, AllowPatterns: allowPatterns, DenyPatterns: denyPatterns}
if err := policy.Validate(policyConfig); err != nil {
    return writeInvalidArgs(cmd, err.Error(), "")
}
normalizedMode := policy.NormalizeMode(mode)
```

Set fields on `session.RegistryEntry`:

```go
PolicyMode:    normalizedMode,
AllowPatterns: allowPatterns,
DenyPatterns:  denyPatterns,
```

Add response field:

```go
"policy_mode": normalizedMode,
```

Add flags:

```go
cmd.Flags().StringVar(&mode, "mode", policy.ModeUnrestricted, "session policy mode: unrestricted, readonly, restricted")
cmd.Flags().StringArrayVar(&allowPatterns, "allow", nil, "allowed command regex for restricted mode")
cmd.Flags().StringArrayVar(&denyPatterns, "deny", nil, "denied command regex for readonly or restricted mode")
```

- [ ] **Step 5: Enforce policy in session exec**

In `newSessionExecCommand`, after `userCommand := remoteCommand(args)` and before `safety.CheckCommand`, add:

```go
policyResult := policy.Evaluate(userCommand, policy.Config{
    Mode:          entry.PolicyMode,
    AllowPatterns: entry.AllowPatterns,
    DenyPatterns:  entry.DenyPatterns,
})
if !policyResult.Allowed {
    return writeError(cmd, "policy_denied", "session policy denied command", policyResult.Message)
}
```

- [ ] **Step 6: Run session tests**

Run:

```bash
go test ./internal/cli -run 'TestSessionOpenStoresPolicyConfig|TestSessionExecPolicy|TestSessionExecConfirmDanger|TestSessionExecRestrictedAllowed' -v
```

Expected: PASS.

- [ ] **Step 7: Commit session CLI changes**

Run:

```bash
gofmt -w internal/cli/session.go internal/cli/session_test.go
git add internal/cli/session.go internal/cli/session_test.go
git commit -m "Enforce policy on session exec"
```

Expected: commit succeeds.

## Task 5: Connect and Connect-Info Policy Flags

**Files:**
- Modify: `internal/cli/connect.go`
- Modify: `internal/cli/connect_test.go`

- [ ] **Step 1: Write failing connect policy test**

Add this test to `internal/cli/connect_test.go`:

```go
func TestConnectPassesPolicyIntoBootstrap(t *testing.T) {
    original := newBootstrapService
    t.Cleanup(func() { newBootstrapService = original })
    newBootstrapService = func() bootstrap.Service {
        return bootstrap.Service{
            EnsureKeyPair: func(string) error { return nil },
            RunSSH: func(_ context.Context, _ bootstrap.SSHTarget, _ string) bootstrap.SSHResult {
                return bootstrap.SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
            },
            NewID: func() (string, error) { return "abc12345", nil },
        }
    }

    var out bytes.Buffer
    cmd := NewRootCommand()
    cmd.SetOut(&out)
    cmd.SetErr(&out)
    cmd.SetArgs([]string{"connect", "--host", "10.0.0.1", "--mode", "restricted", "--allow", `^ls\b`, "--deny", `--force`})

    if err := cmd.Execute(); err != nil {
        t.Fatalf("Execute() error = %v, output=%q", err, out.String())
    }
    var got map[string]any
    if err := json.Unmarshal(out.Bytes(), &got); err != nil {
        t.Fatalf("Unmarshal() error = %v", err)
    }
    if got["policy_mode"] != "restricted" {
        t.Fatalf("policy_mode=%#v want restricted", got["policy_mode"])
    }
    entry, err := session.LoadRegistry(stateBaseDir(), "abc12345")
    if err != nil {
        t.Fatalf("LoadRegistry() error = %v", err)
    }
    if entry.PolicyMode != "restricted" || !slices.Equal(entry.AllowPatterns, []string{`^ls\b`}) || !slices.Equal(entry.DenyPatterns, []string{`--force`}) {
        t.Fatalf("entry policy mode=%q allow=%#v deny=%#v", entry.PolicyMode, entry.AllowPatterns, entry.DenyPatterns)
    }
}
```

Add imports to `internal/cli/connect_test.go`:

```go
import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "slices"
    "strings"
    "testing"

    "github.com/izzzzzi/agent-assh/internal/bootstrap"
    "github.com/izzzzzi/agent-assh/internal/session"
)
```

- [ ] **Step 2: Run connect test to verify failure**

Run:

```bash
go test ./internal/cli -run TestConnectPassesPolicyIntoBootstrap -v
```

Expected: FAIL because `connect` does not know `--mode`, `--allow`, or `--deny`.

- [ ] **Step 3: Add shared policy flag binder**

In `internal/cli/connect.go`, import policy:

```go
    "github.com/izzzzzi/agent-assh/internal/policy"
```

Add helper near `runConnect`:

```go
func bindPolicyOptions(cmd *cobra.Command, req *bootstrap.Request) {
    cmd.Flags().StringVar(&req.PolicyMode, "mode", policy.ModeUnrestricted, "session policy mode: unrestricted, readonly, restricted")
    cmd.Flags().StringArrayVar(&req.AllowPatterns, "allow", nil, "allowed command regex for restricted mode")
    cmd.Flags().StringArrayVar(&req.DenyPatterns, "deny", nil, "denied command regex for readonly or restricted mode")
}
```

Call it in `newConnectCommand` after existing flags:

```go
bindPolicyOptions(cmd, &req)
```

Call it in `newConnectInfoCommand` after existing flags:

```go
bindPolicyOptions(cmd, &req)
```

- [ ] **Step 4: Run connect test**

Run:

```bash
go test ./internal/cli -run TestConnectPassesPolicyIntoBootstrap -v
```

Expected: PASS.

- [ ] **Step 5: Commit connect flags**

Run:

```bash
gofmt -w internal/cli/connect.go internal/cli/connect_test.go
git add internal/cli/connect.go internal/cli/connect_test.go
git commit -m "Add policy flags to connect"
```

Expected: commit succeeds.

## Task 6: Documentation and Prompt Updates

**Files:**
- Modify: `README.md`
- Modify: `README.en.md`
- Modify: `AGENT_INSTRUCTIONS.md`
- Modify: `SYSTEM_PROMPT_snippet.md`
- Modify: `internal/cli/prompt.go`
- Modify: `internal/cli/root_test.go`

- [ ] **Step 1: Write failing prompt manifest test**

In `internal/cli/root_test.go`, update the prompt manifest assertions to require these strings:

```go
for _, want := range []string{
    "policy_mode",
    "--mode readonly",
    "--mode restricted",
    "policy_denied",
    "--confirm-danger does not bypass policy",
} {
    if !strings.Contains(out.String(), want) {
        t.Fatalf("prompt manifest missing %q in %s", want, out.String())
    }
}
```

- [ ] **Step 2: Run prompt tests to verify failure**

Run:

```bash
go test ./internal/cli -run 'TestPrompt' -v
```

Expected: FAIL because docs and prompt output do not mention policy modes.

- [ ] **Step 3: Update `internal/cli/prompt.go`**

Add this paragraph to `agentPrompt` after the `session read` examples:

```text
For safer sessions, use policy modes at connect/open time:
assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME --mode readonly
assh session open -H HOST -u USER --mode restricted --allow '^journalctl ' --allow '^docker logs '

If session exec returns policy_denied, do not change policy, add broad --allow patterns, or retry with --confirm-danger unless the user explicitly asks. --confirm-danger does not bypass policy.
```

Add these strings to `safety_rules`:

```go
"Use --mode readonly or --mode restricted for safer sessions when appropriate.",
"Do not change session policy or add broad allow patterns unless the user explicitly asks.",
"--confirm-danger does not bypass policy.",
```

Add these commands to the manifest:

```go
"connect_readonly":    "assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME --mode readonly",
"session_open_policy": "assh session open -H HOST -u USER --mode restricted --allow '^journalctl '",
```

- [ ] **Step 4: Update markdown docs**

In `README.en.md`, add to Security:

```markdown
- Sessions can be opened with `--mode readonly` or `--mode restricted` to enforce command policy before SSH. `--confirm-danger` does not bypass policy.
```

In `README.en.md`, add a short section after Security:

````markdown
## Policy Modes

Use `--mode readonly` for diagnostic sessions and `--mode restricted` for allowlist-only sessions:

```bash
assh connect -H 203.0.113.10 -u root -E TARGET_PASS -n prod --mode readonly
assh session open -H 203.0.113.10 -u root --mode restricted --allow '^journalctl ' --allow '^docker logs '
```

If `session exec` returns `policy_denied`, ask the user before changing policy or adding broad allow patterns.
````

Add equivalent Russian text to `README.md`.

In `AGENT_INSTRUCTIONS.md`, add:

```markdown
- Prefer `--mode readonly` for diagnostic work when mutation is not needed.
- If `session exec` returns `policy_denied`, do not change policy or add broad `--allow` patterns unless the user explicitly asks.
- `--confirm-danger` does not bypass policy.
```

In `SYSTEM_PROMPT_snippet.md`, add the same policy instructions to the safety section.

- [ ] **Step 5: Run docs and prompt tests**

Run:

```bash
go test ./internal/cli -run 'TestPrompt' -v
npx --yes markdownlint-cli2 --config .markdownlint-cli2.yaml README.md README.en.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
```

Expected: PASS and markdownlint reports 0 errors.

- [ ] **Step 6: Commit docs**

Run:

```bash
gofmt -w internal/cli/prompt.go internal/cli/root_test.go
git add README.md README.en.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md internal/cli/prompt.go internal/cli/root_test.go
git commit -m "Document session policy modes"
```

Expected: commit succeeds.

## Task 7: Full Verification

**Files:**
- No source edits expected.

- [ ] **Step 1: Run full Go test suite**

Run:

```bash
go test ./...
```

Expected: PASS for all packages, including `internal/policy`.

- [ ] **Step 2: Run smoke test**

Run:

```bash
npm run smoke
```

Expected: `smoke ok`.

- [ ] **Step 3: Run release-adjacent check**

Run:

```bash
npm run check
```

Expected: command exits 0 after gofmt, go vet, go test, smoke, npm pack dry-run, and markdownlint.

- [ ] **Step 4: Inspect final diff**

Run:

```bash
git status --short
git log --oneline -8
```

Expected: working tree clean and recent commits show the policy implementation tasks.

## Self-Review

Spec coverage:

- Policy modes are implemented by Tasks 1-5.
- Backward compatibility is covered by empty mode normalization and default `--mode unrestricted`.
- Pre-SSH enforcement and no sequence mutation are covered by Task 4 CLI tests.
- Stable JSON errors are covered by Task 4.
- Documentation and agent behavior are covered by Task 6.
- Full verification is covered by Task 7.

Placeholder scan: no TBD/TODO/fill-in-later placeholders remain.

Type consistency: `policy.Config`, `policy.Result`, `PolicyMode`, `AllowPatterns`, `DenyPatterns`, `policy_denied`, and `dangerous_command_requires_confirmation` are used consistently across tasks.
