# assh Session Reachability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make stale or unreachable `assh` sessions fail with honest, actionable diagnostics instead of implying `expired=false` means live.

**Architecture:** Keep registry TTL logic local and cheap. Add a tiny CLI helper that maps registered-session SSH/tmux failures to session-specific errors and reconnect hints. Preserve existing command behavior and JSON fields, only adding compatibility-safe fields to `session list`.

**Tech Stack:** Go CLI with Cobra, existing `internal/session` registry, existing `internal/transport` result handling, existing `response.OK` JSON output, existing npm/go validation commands.

## Global Constraints

- Do not implement automatic reconnect.
- Do not store passwords or add a credential store.
- Preserve existing `expired` field semantics and compatibility.
- Hints must not contain secrets.
- Keep changes minimal and covered by tests.
- Release gate remains `npm run check`.

---

## File Structure

- `internal/cli/session.go`: add a helper for registered-session lifecycle errors and use it in `session exec` / `session read` paths.
- `internal/cli/session_async.go`, `internal/cli/session_ps.go`, `internal/cli/session_service.go`, `internal/cli/session_db.go`, `internal/cli/session_docker.go`: optional follow-up callers that use registered sessions; only update if tests or grep show they still return misleading raw lifecycle errors.
- `internal/cli/session_list.go`: add `status_basis: "ttl_only"` and `reachable: "unknown"` to list output.
- `internal/cli/session_test.go`: add tests for list fields, unreachable SSH, auth failure hint, and remote stale tmux signal.
- `skills/assh/SKILL.md`, `skills/assh/references/session.md`, `AGENT_INSTRUCTIONS.md`, `SYSTEM_PROMPT_snippet.md`: update agent-facing recovery guidance after code behavior exists.

---

### Task 1: Session list says TTL-only, not live

**Files:**
- Modify: `internal/cli/session_list.go`
- Modify: `internal/cli/session_test.go`

**Interfaces:**
- Consumes: existing `session.Metadata.Expired(now)`.
- Produces: each `assh session list` item includes existing `expired` plus new fields `status_basis` and `reachable`.

- [ ] **Step 1: Add failing test assertions**

In `internal/cli/session_test.go`, extend `TestSessionListReturnsSortedSessions` after the existing `expired` assertion:

```go
	if first["status_basis"] != "ttl_only" || first["reachable"] != "unknown" {
		t.Fatalf("session list must say liveness is not checked: %#v", first)
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestSessionListReturnsSortedSessions -count=1
```

Expected: FAIL because `status_basis` and `reachable` are missing.

- [ ] **Step 3: Add minimal fields**

In `internal/cli/session_list.go`, add two fields inside the `response.OK{...}` session item:

```go
					"status_basis": "ttl_only",
					"reachable":    "unknown",
```

The block should include:

```go
				sessions = append(sessions, response.OK{
					"sid":          entry.SID,
					"session":      entry.Label,
					"host":         entry.Host,
					"user":         entry.User,
					"port":         entry.Port,
					"created_at":   entry.CreatedAt,
					"ttl_seconds":  entry.TTLSeconds,
					"seq":          entry.Seq,
					"tmux_name":    entry.TmuxName,
					"expired":      expired,
					"status_basis": "ttl_only",
					"reachable":    "unknown",
				})
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/cli -run TestSessionListReturnsSortedSessions -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/session_list.go internal/cli/session_test.go
git commit -m "feat: label session list liveness as ttl-only"
```

---

### Task 2: Registered session SSH failures return `session_unreachable`

**Files:**
- Modify: `internal/cli/session.go`
- Modify: `internal/cli/session_test.go`

**Interfaces:**
- Consumes: existing `lifecycleResultErrorCode(ctx.Err(), result)` and `sshResultErrorMessage(ctx.Err(), result)`.
- Produces: helper `registeredSessionLifecycleError(cmd *cobra.Command, entry session.RegistryEntry, code string, message string) error` used by session commands.

- [ ] **Step 1: Add failing test for connection closed**

Add this test to `internal/cli/session_test.go` near other session exec tests:

```go
func TestSessionExecConnectionClosedReturnsSessionUnreachable(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	entry := session.RegistryEntry{
		SID:           "abcdef12",
		Label:         "likeboom",
		Host:          "80.74.27.102",
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
	runSSH = func(ctx context.Context, args []string, remoteCommand string) transport.Result {
		return transport.Result{ExitCode: 255, Stderr: []byte("Connection closed by 80.74.27.102 port 22")}
	}
	defer func() { runSSH = oldRunSSH }()

	got := executeSessionJSON(t, []string{"session", "exec", "-s", "abcdef12", "--", "pwd"})

	if got["ok"] != false || got["error"] != "session_unreachable" {
		t.Fatalf("unexpected response: %#v", got)
	}
	hint, _ := got["hint"].(string)
	for _, want := range []string{"assh connect", "-i KEY", "-E PASSWORD_ENV", "--ssh-config ALIAS", "80.74.27.102", "root"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint %q missing %q", hint, want)
		}
	}
}
```

Ensure imports include `context`, `strings`, and `github.com/izzzzzi/agent-assh/internal/transport` if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestSessionExecConnectionClosedReturnsSessionUnreachable -count=1
```

Expected: FAIL because current error is `connection_error`.

- [ ] **Step 3: Add helper implementation**

In `internal/cli/session.go`, add this helper near `remoteCommand` or other session helpers:

```go
func registeredSessionLifecycleError(cmd *cobra.Command, entry session.RegistryEntry, code string, message string) error {
	if code == "" {
		return nil
	}
	hint := reconnectHint(entry)
	switch code {
	case "auth_failed", "connection_error", "host_key_failed", "timeout", "ssh_missing":
		return writeError(cmd, "session_unreachable", "registered session could not be reached over SSH: "+message, hint)
	case "tmux_missing":
		return writeError(cmd, "session_stale", "registered session host is reachable but tmux is unavailable: "+message, hint)
	default:
		if strings.Contains(strings.ToLower(message), "session_not_found") || strings.Contains(strings.ToLower(message), "tmux_send_failed") {
			return writeError(cmd, "session_stale", "registered remote tmux session is gone: "+message, hint)
		}
		return writeError(cmd, code, message, "")
	}
}

func reconnectHint(entry session.RegistryEntry) string {
	name := entry.Label
	if name == "" {
		name = entry.SID
	}
	host := entry.Host
	user := entry.User
	if host == "" {
		host = "HOST"
	}
	if user == "" {
		user = "USER"
	}
	return "do not keep retrying this SID; reconnect with explicit auth: assh connect -H " + host + " -u " + user + " -i KEY -n " + name + " or assh connect -H " + host + " -u " + user + " -E PASSWORD_ENV -n " + name + "; prefer assh connect --ssh-config ALIAS -n " + name + " for repeat use"
}
```

- [ ] **Step 4: Use helper in `session exec`**

In `newSessionExecCommand`, replace:

```go
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
```

with:

```go
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return registeredSessionLifecycleError(cmd, entry, code, sshResultErrorMessage(ctx.Err(), result))
			}
```

- [ ] **Step 5: Run test to verify it passes**

Run:

```bash
go test ./internal/cli -run TestSessionExecConnectionClosedReturnsSessionUnreachable -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/session.go internal/cli/session_test.go
git commit -m "feat: report unreachable registered sessions"
```

---

### Task 3: Remote tmux-gone errors return stale-session guidance

**Files:**
- Modify: `internal/cli/session.go`
- Modify: `internal/cli/session_test.go`

**Interfaces:**
- Consumes: `registeredSessionLifecycleError` from Task 2.
- Produces: stale remote tmux/session errors map to `session_stale` with reconnect hint.

- [ ] **Step 1: Add failing stale tmux test**

Add this test to `internal/cli/session_test.go`:

```go
func TestSessionExecRemoteSessionNotFoundReturnsSessionStale(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	entry := session.RegistryEntry{
		SID:           "abcdef12",
		Label:         "likeboom",
		Host:          "80.74.27.102",
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
	runSSH = func(ctx context.Context, args []string, remoteCommand string) transport.Result {
		return transport.Result{ExitCode: 3, Stderr: []byte("session_not_found")}
	}
	defer func() { runSSH = oldRunSSH }()

	got := executeSessionJSON(t, []string{"session", "exec", "-s", "abcdef12", "--", "pwd"})

	if got["ok"] != false || got["error"] != "session_stale" {
		t.Fatalf("unexpected response: %#v", got)
	}
	hint, _ := got["hint"].(string)
	if !strings.Contains(hint, "do not keep retrying this SID") || !strings.Contains(hint, "--ssh-config ALIAS") {
		t.Fatalf("unexpected hint: %q", hint)
	}
}
```

- [ ] **Step 2: Run test to verify behavior**

Run:

```bash
go test ./internal/cli -run TestSessionExecRemoteSessionNotFoundReturnsSessionStale -count=1
```

Expected before Task 2 helper handles message: FAIL with `command_failed`. Expected after correct helper: PASS.

- [ ] **Step 3: Adjust helper only if needed**

If the test fails because `lifecycleResultErrorCode` returns `command_failed`, ensure the `default` branch in `registeredSessionLifecycleError` checks lowercased `message` for `session_not_found` and `tmux_send_failed` before returning the original code:

```go
	default:
		lower := strings.ToLower(message)
		if strings.Contains(lower, "session_not_found") || strings.Contains(lower, "tmux_send_failed") {
			return writeError(cmd, "session_stale", "registered remote tmux session is gone: "+message, hint)
		}
		return writeError(cmd, code, message, "")
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./internal/cli -run 'TestSessionExec(ConnectionClosedReturnsSessionUnreachable|RemoteSessionNotFoundReturnsSessionStale)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/session.go internal/cli/session_test.go
git commit -m "feat: report stale remote tmux sessions"
```

---

### Task 4: Apply session error mapping to read path and docs

**Files:**
- Modify: `internal/cli/session.go`
- Modify: `internal/cli/session_test.go`
- Modify: `skills/assh/SKILL.md`
- Modify: `skills/assh/references/session.md`
- Modify: `AGENT_INSTRUCTIONS.md`
- Modify: `SYSTEM_PROMPT_snippet.md`

**Interfaces:**
- Consumes: `registeredSessionLifecycleError` and `reconnectHint` from Task 2.
- Produces: `session read` uses the same unreachable/stale behavior; docs tell agents how to recover.

- [ ] **Step 1: Add failing test for `session read` SSH failure**

Add this test to `internal/cli/session_test.go`:

```go
func TestSessionReadConnectionClosedReturnsSessionUnreachable(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	entry := session.RegistryEntry{
		SID:           "abcdef12",
		Label:         "likeboom",
		Host:          "80.74.27.102",
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
	runSSH = func(ctx context.Context, args []string, remoteCommand string) transport.Result {
		return transport.Result{ExitCode: 255, Stderr: []byte("Connection closed by 80.74.27.102 port 22")}
	}
	defer func() { runSSH = oldRunSSH }()

	got := executeSessionJSON(t, []string{"session", "read", "-s", "abcdef12", "--seq", "1", "--limit", "50"})

	if got["ok"] != false || got["error"] != "session_unreachable" {
		t.Fatalf("unexpected response: %#v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestSessionReadConnectionClosedReturnsSessionUnreachable -count=1
```

Expected: FAIL until `session read` uses the helper.

- [ ] **Step 3: Use helper in `session read`**

In `newSessionReadCommand`, replace its lifecycle error block:

```go
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
```

with:

```go
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return registeredSessionLifecycleError(cmd, entry, code, sshResultErrorMessage(ctx.Err(), result))
			}
```

- [ ] **Step 4: Update docs**

Add this concise rule to `skills/assh/SKILL.md` under session/token economy or security rules:

```markdown
Stale sessions: `expired=false` is TTL-only, not proof the SSH/tmux channel is alive. If `session_unreachable` or `session_stale` appears, stop retrying that SID and reconnect with explicit auth: `-i KEY`, `-E PASSWORD_ENV`, or `--ssh-config ALIAS`.
```

Add the same rule to `skills/assh/references/session.md`, `AGENT_INSTRUCTIONS.md`, and `SYSTEM_PROMPT_snippet.md` near existing session-list/session-read guidance.

- [ ] **Step 5: Run focused tests**

Run:

```bash
go test ./internal/cli -run 'TestSession(ListReturnsSortedSessions|ExecConnectionClosedReturnsSessionUnreachable|ExecRemoteSessionNotFoundReturnsSessionStale|ReadConnectionClosedReturnsSessionUnreachable)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run full validation**

Run:

```bash
npm run check
git diff --check
```

Expected: both pass.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/session.go internal/cli/session_test.go skills/assh/SKILL.md skills/assh/references/session.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
git commit -m "docs: teach stale session recovery"
```

---

## Self-Review Checklist

- Spec coverage: Task 1 covers TTL-only list semantics; Tasks 2-4 cover session unreachable/stale errors; Task 4 covers docs.
- No auto-reconnect is included.
- No password storage is included.
- Tests are concrete and fail before implementation.
- Hints include `-i KEY`, `-E PASSWORD_ENV`, and `--ssh-config ALIAS` without secrets.
- Validation uses `npm run check` and `git diff --check`.
