# assh Agent Bootstrap Design

Date: 2026-05-14
Status: Awaiting user review

## Context

`assh` is being prepared for its first public `v1.0.0` release. The current Go CLI already provides token-efficient SSH execution, paginated reads, tmux-backed sessions, key deployment, audit logs, GoReleaser configuration, npm packaging, and bilingual release documentation.

The remaining product gap is first-contact usability for agents. Today an agent must know the sequence:

1. Try SSH with a key.
2. Deploy a key with `key-deploy` if password access is needed.
3. Check capabilities.
4. Open a session with `--install-tmux`.
5. Run cleanup separately.

The desired v1 behavior is an agent-first SSH CLI where a user can install `assh`, give an agent minimal instructions, and the agent can safely bootstrap access, prepare tmux, open a session, and manage old sessions without pulling large output into context.

The old Bash MVP existed during development, but the Go binary is now the only supported implementation.

## Goals

- Add a high-level `assh connect` command as the default first step for agents.
- Support first login with a password from an environment variable when key access is not available.
- Generate or reuse an agent SSH key, deploy the public key, and verify subsequent key-based login.
- Check remote capabilities and prepare tmux for persistent sessions.
- Open a trusted tmux session after bootstrap succeeds.
- Run safe cleanup of old trusted `assh` sessions during bootstrap.
- Keep low-level commands available for targeted use.
- Remove the old Bash MVP and related documentation references.
- Update README, Russian README, agent instructions, system prompt snippet, and help text around the `connect` workflow.
- Preserve the existing `v1.0.0` release target because no public v1 tag has been shipped yet.

## Non-Goals

- Do not add interactive password prompts in v1.0.
- Do not accept passwords through command-line arguments.
- Do not implement a native SSH backend.
- Do not add SSH profiles or a config file in this iteration.
- Do not support persistent sessions on Windows remote hosts.
- Do not install packages interactively or silently outside an explicit bootstrap flow.
- Do not remove engineering history under `docs/superpowers`.

## User-Facing Workflow

Primary command:

```bash
export TARGET_PASS="..."
assh connect -H HOST -u root -E TARGET_PASS -n deploy
unset TARGET_PASS
```

Typical key-only command:

```bash
assh connect -H HOST -u root -i ~/.ssh/id_agent_ed25519 -n deploy
```

`connect` performs the full first-contact workflow:

1. Resolve identity path. If `-i` is not provided, use `~/.ssh/id_agent_ed25519`.
2. Ensure a keypair exists at that identity path.
3. Try key-based SSH.
4. If key login works, do not read or use the password environment variable.
5. If key login fails and `-E` is not provided, return `auth_failed` with a hint.
6. If key login fails and `-E` is provided, use the password from that environment variable only to deploy the public key.
7. Verify that key-based login works after deployment.
8. Probe remote capabilities.
9. Ensure tmux is available. If tmux is missing, attempt explicit non-interactive installation as part of `connect`.
10. Run safe session cleanup for old trusted `assh` sessions.
11. Open a trusted tmux session.
12. Return one JSON object with the bootstrap result.

Successful response shape:

```json
{
  "ok": true,
  "host": "10.0.0.1",
  "user": "root",
  "identity": "/home/user/.ssh/id_agent_ed25519",
  "key_deployed": true,
  "key_verified": true,
  "tmux_installed": true,
  "gc_deleted": ["abcdef12"],
  "sid": "f7a2b3c4",
  "session": "deploy",
  "tmux_name": "assh_f7a2b3c4"
}
```

The command includes `next_commands` so agents can continue without reading documentation:

```json
{
  "next_commands": {
    "exec": "assh session exec -s f7a2b3c4 -- \"pwd\"",
    "read": "assh session read -s f7a2b3c4 --seq 1 --limit 50",
    "close": "assh session close -s f7a2b3c4"
  }
}
```

## CLI Contract

`assh connect` flags:

- `-H, --host`: required SSH host.
- `-u, --user`: SSH user, default `root`.
- `-p, --port`: SSH port, default `22`.
- `-i, --identity`: identity file, default `~/.ssh/id_agent_ed25519`.
- `-E, --password-env`: environment variable containing the first-login password.
- `-n, --name`: session label.
- `--ttl`: session TTL, default `12h`.
- `--timeout`: SSH/bootstrap timeout in seconds, default `300`.
- `--host-key-policy`: `accept-new`, `strict`, or `no-check`; default `accept-new`.
- `--gc-older-than`: cleanup age threshold, default `24h`.
- `--no-gc`: skip bootstrap cleanup.
- `--no-install-tmux`: do not install tmux; return `tmux_missing` if absent.

`connect` is an orchestration command. It uses the same stable JSON error style as other commands:

```json
{"ok":false,"error":"auth_failed","message":"key login failed and no password env was provided","hint":"retry with -E PASSWORD_ENV or configure SSH keys"}
```

Important error codes:

- `invalid_args`
- `ssh_missing`
- `auth_failed`
- `host_key_failed`
- `connection_error`
- `timeout`
- `key_deploy_failed`
- `tmux_missing`
- `tmux_install_failed`
- `session_not_found`
- `command_failed`
- `internal_error`

## Security Model

Passwords:

- Passwords are accepted only through `-E/--password-env`.
- `connect` never accepts `--password`.
- `connect` never logs password values.
- The password is used only for key deployment.
- If key login already works, the password variable is not read.
- After key deployment, key-based login must be verified before opening a session.

Host keys:

- Default host key policy remains `accept-new`.
- `strict` is supported for hardened environments.
- `no-check` remains available but documentation must mark it unsafe and suitable only for disposable lab/dev cases.

tmux installation:

- `connect` installs tmux by default as part of explicit bootstrap unless `--no-install-tmux` is set.
- Installation must be non-interactive.
- `sudo -n` failures or unsupported package managers return machine-readable errors instead of hanging.
- Package installation failures emit `tmux_install_failed`.
- If `--no-install-tmux` is set and tmux is absent, return `tmux_missing`.

Cleanup:

- Cleanup deletes only sessions with trusted `assh` metadata.
- Remote metadata must validate exact fields: `created_by`, `sid`, and `tmux_name`.
- Cleanup must not kill unrelated tmux sessions.
- Local registry is deleted only after remote cleanup succeeds or the remote session is already absent.

Audit:

- Audit logs must not include password values.
- Audit logs must not include raw command text.
- Audit records action, host, user, exit code, line counts, and command hash when those fields apply to the action.

## Architecture

Add a focused connect layer rather than turning every command into a large orchestration path.

Files:

- `internal/cli/connect.go`: Cobra command and JSON response assembly.
- `internal/bootstrap/service.go`: orchestration service for key verification, key deployment, tmux preparation, cleanup, and session open.
- `internal/bootstrap/service_test.go`: unit tests for bootstrap decisions.
- Existing `internal/cli/misc.go`: keep low-level `key-deploy`, but move reusable key/password helpers if the file becomes too dense.
- Existing `internal/session/service.go`: reuse session metadata, open, close, and GC helpers.
- Existing `internal/capabilities/service.go`: reuse capability probe.

The service is testable with fake SSH runners. Unit tests do not require real SSH.

`connect` reuses existing low-level behavior through internal services/functions. It does not shell out to the local `assh` binary.

## Legacy Cleanup

Remove:

- `assh.bash`

Documentation updates:

- Remove references saying the Bash MVP is kept for comparison.
- Present the Go binary as the only supported implementation.
- Keep engineering design/plan documents under `docs/superpowers` as project history.

## Documentation

README and README.ru start with `assh connect`.

Required documentation sections:

- What `assh` does.
- Quick install through `npm i -g agent-assh`.
- First connection with password-to-key bootstrap.
- Key-only connection.
- Agent workflow after connect: `session exec`, `session read`, `session close`, `session gc`.
- Token economy examples.
- Persistent tmux session behavior.
- Safe cleanup behavior.
- Security notes.
- Advantages and limitations.
- Manual GitHub Release install.

Agent docs:

- `AGENT_INSTRUCTIONS.md` is short and operational.
- First SSH action is `assh connect`.
- Agents prefer `session exec` and `session read` after connect.
- Agents use `read --raw` only for piping/exact output.
- Agents close sessions when done and run `session gc` for stale sessions.

Help text:

- `assh --help` makes `connect` visible as the first obvious entry point.
- `assh connect --help` includes examples and explains password env usage.

## Release Requirements

This remains part of the first public `v1.0.0` release.

Before publishing:

- Configure the intended GitHub remote.
- Ensure `goreleaser check` passes in a repo with remote configured.
- Ensure `goreleaser release --snapshot --clean` builds all configured artifacts.
- Ensure npm package `agent-assh` downloads the matching release archives.
- Ensure `package.json` version and tag match.
- Ensure GitHub repository has `NPM_TOKEN` for release workflow.

## Testing Strategy

Add tests for:

- `connect` rejects missing host.
- `connect` uses existing key login and does not read password env when key login succeeds.
- `connect` returns `auth_failed` when key login fails and no password env is provided.
- `connect` deploys key when key login fails and password env is provided.
- `connect` verifies key login after deployment before opening session.
- `connect` returns `key_deploy_failed` when deployment succeeds but key verification fails.
- `connect` installs tmux when missing and install is possible.
- `connect` returns `tmux_missing` when tmux is missing and `--no-install-tmux` is set.
- `connect` returns `tmux_install_failed` when non-interactive install fails.
- `connect` opens a session and returns `sid` and `tmux_name`.
- `connect` runs safe cleanup unless `--no-gc` is set.
- `assh.bash` is removed and not referenced by release docs.
- README and agent docs mention `connect` as the first workflow.

Verification commands:

```bash
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go test -race ./...
npx markdownlint-cli2 "*.md" "docs/**/*.md"
npm run smoke
npm pack --dry-run
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
go run github.com/rhysd/actionlint/cmd/actionlint@latest
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

`goreleaser check` must pass once a GitHub remote is configured.

## Acceptance Criteria

- `assh connect` is the documented first step for agents.
- A user can bootstrap password-based first access into key-based access.
- A successful `connect` opens a tmux session ready for `session exec`.
- tmux is prepared non-interactively or a stable error is returned.
- Stale trusted sessions can be cleaned safely.
- Old Bash MVP is removed.
- Docs are readable, bilingual, and honest about advantages and limitations.
- GoReleaser and npm packaging remain aligned with `v1.0.0`.
- Full verification passes except for remote-dependent publishing checks when no git remote is configured.
