# assh Go CLI Design

Date: 2026-05-14
Status: Approved for implementation planning

## Context

`assh` is currently a single Bash script that wraps SSH for LLM agents. It provides two core capabilities:

- Token economy: `exec` stores large stdout/stderr locally and returns metadata; `read` pages selected output.
- Persistent sessions: `session` uses remote `tmux` so cwd/env survive across related commands.

The current project is compact and well documented, but the Bash implementation has high-risk areas: manual JSON construction, shell quoting issues, `eval` in audit filtering, fragile `tmux send-keys` command construction, and no automated tests.

The next version should keep the product idea and agent-facing workflow, but replace the Bash implementation with a cross-platform binary.

## Goals

- Build `assh` v2 as a Go CLI distributed as one binary for Windows, macOS, and Linux.
- Treat the CLI as a stable function surface for agents, not as a human-first terminal app.
- Keep the current command model where practical: `exec`, `read`, `session`, `scan`, `key-deploy`, `audit`.
- Return strict machine-readable JSON by default.
- Avoid local shell interpolation for launching SSH.
- Provide safe lifecycle management for remote `tmux` sessions.
- Support explicit, safe `tmux` bootstrapping when a remote Linux/Unix host lacks `tmux`.

## Non-Goals

- Do not implement a native SSH transport in v2 unless system OpenSSH becomes a blocker.
- Do not support persistent sessions for Windows remote hosts in the first implementation.
- Do not silently install packages on remote hosts.
- Do not preserve Bash internals or file formats where they create safety problems.

## Language and Runtime

Use Go.

Reasons:

- Cross-compiles cleanly to Windows, macOS, and Linux.
- Produces a single binary without requiring Python or a shell runtime.
- Has strong standard library support for JSON, process execution, contexts, timeouts, file paths, and tests.
- Is faster to implement and maintain than Rust for this CLI.
- Avoids Python packaging complexity when the desired artifact is a portable binary.

## SSH Backend

Use system OpenSSH as the v2 transport backend.

Go should call `ssh` via `exec.CommandContext` with an argv array. It must not build local command strings and pass them through a shell.

Benefits:

- Preserves compatibility with existing OpenSSH behavior.
- Works with `~/.ssh/config`, `ssh-agent`, `ProxyJump`, bastion hosts, known_hosts, ControlMaster, and common key formats.
- Reduces implementation risk compared with a native SSH library.

Limitations:

- The local machine must have an OpenSSH client available.
- Remote command execution still requires careful remote shell quoting.

If native SSH is needed later, design the transport behind a small interface so a future backend can be added without changing the command contract.

## Agent-Facing Contract

JSON is the default output for all operational commands.

Rules:

- Each command returns exactly one JSON object by default.
- Success responses include `ok: true`.
- Error responses include `ok: false`, stable `error`, and optional `message`, `hint`, and `details`.
- Human prose is limited to `help` or explicit `--human`.
- Commands must not ask interactive questions.
- Long stdout/stderr never appears in `exec` or `session exec` responses. Those commands return IDs, line counts, exit status, and metadata.
- Output payloads are read through `read` or `session read` with pagination.

Example error:

```json
{"ok":false,"error":"tmux_missing","message":"tmux is not installed","hint":"retry with --install-tmux"}
```

Primary agent functions:

- `assh exec`
- `assh read`
- `assh session open`
- `assh session exec`
- `assh session read`
- `assh session close`
- `assh session gc`
- `assh capabilities`
- `assh scan`
- `assh key-deploy`
- `assh audit`

## Command Shape

The CLI should remain close to the current interface:

```bash
assh exec -H host -u root -i key -- "journalctl -p warning"
assh read --id output_id --limit 20 --offset 0

assh session open -H host -n deploy --ttl 4h --install-tmux
assh session exec -s sid -- "cd /app"
assh session read -s sid --seq 2 --limit 20
assh session close -s sid
assh session gc --host host --older-than 24h --execute

assh capabilities -H host
assh scan -H host
assh key-deploy -H host -E PASS_ENV
assh audit --last 20
```

Small CLI improvements are allowed if they improve safety or machine readability. Avoid breaking the current agent instructions without a clear reason.

## Local State

Store local state under the user config/cache area appropriate to the OS.

Suggested defaults:

- Linux: `${XDG_STATE_HOME:-~/.local/state}/assh`
- macOS: `~/Library/Application Support/assh`
- Windows: `%LOCALAPPDATA%\assh`

State categories:

- `outputs`: stdout/stderr files from `exec`.
- `sessions`: local registry mapping `sid` to host/user/label/tmux_name/ttl/created_at.
- `audit`: JSONL audit log.
- `config`: defaults for timeout, retry count, output retention, session TTL, and host key policy.

All local JSON files must be written with atomic write semantics where practical: write temp file, fsync if feasible, rename.

## Remote State

For remote Unix-like hosts, store `assh` session data under:

```text
~/.assh/sessions/<sid>/
```

Each session directory contains:

- `meta.json`
- `<seq>.out`
- `<seq>.err`
- `<seq>.rc`

`meta.json` includes:

```json
{
  "created_by": "assh",
  "sid": "01h...",
  "label": "deploy",
  "tmux_name": "assh_01h...",
  "created_at": "2026-05-14T00:00:00Z",
  "ttl_seconds": 14400,
  "client_id": "local-client-id"
}
```

The user label is never used directly as a shell, path, or tmux identifier.

## Session Lifecycle

`session open`:

- Generates a safe `sid`.
- Uses tmux session name `assh_<sid>`.
- Stores user-provided `-n/--name` only as a label.
- Writes local registry and remote `meta.json`.
- Runs lightweight local cleanup before or after opening.

`session close`:

- Closes only sessions with a matching local registry entry and remote `created_by: "assh"` marker.
- Kills only the exact `tmux_name` from trusted metadata.
- Removes only the matching remote session directory after validating the path shape.
- Removes the local registry entry.

`session gc`:

- Finds stale local and remote `assh` sessions.
- Defaults to dry-run.
- Requires `--execute` for destructive remote cleanup.
- Can filter by host and age.
- Deletes only sessions with valid `assh` metadata.
- Returns a JSON report of candidates and actions taken.

Suggested default TTL: 12 hours. Users can override with `--ttl`.

## tmux Bootstrap

`tmux` installation must be explicit.

Behavior:

- If `tmux` is missing and `--install-tmux` is not set, return `tmux_missing`.
- If `--install-tmux` is set, detect OS/package manager through `capabilities`.
- Supported package managers can include `apt`, `dnf`, `yum`, `apk`, `pacman`, and `brew`.
- If installation needs an interactive password or unsupported privilege escalation, return a machine-readable error instead of hanging.

The command must not silently install packages as a hidden side effect.

## Capabilities

Add `assh capabilities -H host`.

It should detect and return:

- Remote OS family and shell type.
- Whether `tmux` is installed.
- Available package manager.
- Whether non-interactive install appears possible.
- Whether basic filesystem paths for `~/.assh` can be created.
- Whether the remote looks Unix-like or unsupported for sessions.

This command helps agents decide whether to call `session open --install-tmux`, use stateless `exec`, or ask the user for credentials.

## Error Codes

Use stable error codes. Initial set:

- `auth_failed`
- `host_key_failed`
- `connection_error`
- `timeout`
- `command_failed`
- `ssh_missing`
- `tmux_missing`
- `tmux_install_failed`
- `unsupported_remote_session_backend`
- `session_not_found`
- `output_not_found`
- `invalid_args`
- `invalid_json`
- `permission_denied`
- `cleanup_refused`

Errors may include `message`, `hint`, and `details`, but agents should be able to branch on `error`.

## Security

Security rules:

- Do not use local shell execution to call `ssh`.
- Validate host, port, sid, output IDs, stream names, and numeric flags.
- Generate IDs with cryptographically secure randomness.
- Use Go JSON encoding for all JSON output and logs.
- Do not write passwords, private keys, or full secrets to audit logs.
- Treat command logging as configurable because command text may include secrets.
- Avoid `StrictHostKeyChecking=accept-new` being hard-coded. Provide a default policy and machine-readable failures.
- Remote cleanup must verify `created_by: "assh"` and safe path shapes before deletion.
- Remote command construction must go through a narrow, tested quoting layer.

## Testing

Required test layers:

- Unit tests for JSON responses and error shape.
- Unit tests for CLI flag parsing.
- Unit tests for sid/output ID/path validation.
- Unit tests for duration parsing and TTL logic.
- Unit tests for remote command quoting.
- Integration tests using a mock `ssh` binary placed first in `PATH`.
- Integration tests for output paging and local storage.
- Later e2e tests using a Docker SSH container with `tmux`.

The first implementation plan should include tests before or alongside code for the command contract.

## Migration

The Go implementation can coexist with the current Bash script during development.

Suggested approach:

- Keep current `assh` as reference behavior.
- Build Go binary as `assh` during development.
- When command contract is verified, replace `assh` with the Go binary or update install docs.
- Update `README.md`, `AGENT_INSTRUCTIONS.md`, and `SYSTEM_PROMPT_snippet.md` after CLI behavior is stable.

## Implementation Defaults

Use these defaults for the first implementation plan:

- CLI framework: Cobra. The command tree is nested enough that a small framework is justified.
- OS-specific state directories: implement a small internal helper using Go standard library environment variables first. Add a dependency only if edge cases require it.
- Audit command text: do not log full command text by default. Log command hash, host, user, exit code, line counts, and timestamps. Add an opt-in config later if full command audit is needed.
- Host key policy: default to OpenSSH-compatible `accept-new` when supported by the installed `ssh`; expose `--host-key-policy accept-new|strict|no-check` and return machine-readable errors on failure.
- `--json`: accept it as a no-op for explicitness. JSON remains the default for operational commands.
