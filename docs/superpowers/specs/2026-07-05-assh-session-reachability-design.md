# assh Session Reachability Design

## Problem

`assh session list` currently reports `expired=false` from local registry TTL only. Agents can read that as “the session is alive,” but the real SSH ControlMaster, network path, or remote tmux session may already be dead. Then the agent enters a bad loop:

1. stale session looks usable in local metadata;
2. `session exec/read` fails with low-level SSH text such as `Connection closed`;
3. fresh `connect -H HOST -u USER -n NAME` fails with `auth_failed` because the right key (`-i`) or password env (`-E`) was not supplied;
4. the agent does not get a clear recovery command.

This is a runtime UX issue, not just a skill/docs issue.

## Goals

- Make `expired` clearly mean TTL-only, not live reachability.
- When session operations cannot reach SSH or tmux, return a clear, agent-actionable error.
- Preserve safe auth behavior: never store passwords; do not auto-reconnect with hidden credentials.
- Give agents a short next step: reconnect with explicit `-i`, `-E`, or `--ssh-config`.
- Keep the fix small and testable.

## Non-goals

- No automatic reconnect in this iteration.
- No password persistence.
- No new credential store.
- No broad rewrite of session registry or ControlMaster behavior.

## Approaches considered

### A. Honest diagnostics, no auto-reconnect — recommended

Session operations keep using registry metadata, but failures are normalized into clearer errors:

- SSH/network/auth failure while using a registered session returns `session_unreachable` with a reconnect hint.
- Remote tmux missing/session missing returns `session_stale` or keeps `session_not_found` with a better hint.
- `session list` labels TTL state explicitly, e.g. keep `expired` for compatibility and add `status_basis: "ttl_only"` or `reachable: "unknown"`.

Trade-off: agents still need to reconnect explicitly, but they stop trusting `expired=false` as liveness.

### B. Auto-reconnect

On stale/unreachable session, `assh` would try to reconnect and open a new tmux session automatically.

Rejected for now: it requires remembering auth method/key/config alias well enough to retry safely. It risks surprising network/auth behavior and still cannot work for password-only access without a fresh env var.

### C. Profiles/aliases first

Encourage users to create `~/.ssh/config` aliases and reconnect by alias.

Useful guidance, but not sufficient alone: the CLI should still tell agents when a listed session is unreachable.

## Design

### Error model

Add a small session-specific error mapping around registered-session operations (`session exec`, `session read`, and sibling session commands that use registry SSH details):

- If local registry entry is missing: keep `session_not_found`.
- If SSH exits with auth/network/connection failure while using an existing session: return `session_unreachable`.
- If the remote command reaches the host but reports `session_not_found`, `tmux_missing`, or `tmux_send_failed`: return a stale/tmux-specific error with a reconnect hint. The exact code can remain `session_not_found` for compatibility if the hint is improved; the important change is that agents can distinguish “local registry exists but remote session is gone.”

Recommended JSON shape:

```json
{
  "ok": false,
  "error": "session_unreachable",
  "message": "registered session could not be reached over SSH",
  "hint": "reconnect with assh connect -H HOST -u USER -i KEY -n NAME, assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME, or assh connect --ssh-config ALIAS -n NAME"
}
```

For auth failures, include `auth_failed` detail in the message, but keep the top-level session error actionable for agents operating from a SID.

### Session list semantics

Keep existing fields for compatibility, but make the liveness limitation explicit:

```json
{
  "expired": false,
  "status_basis": "ttl_only",
  "reachable": "unknown"
}
```

This avoids expensive network checks in ordinary `session list` and prevents the false implication that `expired=false` means live.

Optionally add a future `session list --check` or `session doctor -s SID` command, but do not include it in the first implementation unless the minimal error/hint work is not enough.

### Hints

Hints should not contain secrets. They should use example values and the stored registry host/user/name where safe:

```text
reconnect with explicit auth: assh connect -H 80.74.27.102 -u root -i KEY -n likeboom or assh connect -H 80.74.27.102 -u root -E PASSWORD_ENV -n likeboom; prefer --ssh-config ALIAS for repeat use
```

If the registry has `Identity`, mention retrying the same identity. If not, explicitly say no identity was stored and an explicit `-i`, `-E`, or `--ssh-config` is required.

### Agent-facing docs

Update the skill/docs after code behavior exists:

- `expired=false` is TTL-only.
- On `session_unreachable`/stale errors, do not keep retrying the SID.
- Reconnect with explicit auth (`-i`, `-E`, or `--ssh-config`) and use the new SID.

## Testing

Use the smallest tests that catch the behavior:

- Unit test error mapping: SSH `Connection closed` / exit 255 on a registered session returns `session_unreachable` with reconnect hint.
- Unit test auth failure on registered session returns actionable reconnect hint including `-i`, `-E`, and `--ssh-config` options.
- Unit test remote `session_not_found` from tmux path returns stale-session guidance.
- Unit test `session list` includes `status_basis: "ttl_only"` and `reachable: "unknown"` while preserving `expired`.
- Existing `npm run check` remains the release gate.

## Rollout

1. Implement the error/hint mapping and list fields.
2. Update agent docs with the new failure recovery rule.
3. Build local tarball for manual testing.
4. If local testing confirms the UX, then tag/release.

## Open decision

Do not implement `session doctor` in the first pass unless manual testing shows the clearer error/hint is still not enough.
