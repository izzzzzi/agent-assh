---
name: assh
description: "Use when SSH, remote commands, logs, file transfer, deploy, or server inspection is needed — bootstraps connections, manages tmux sessions, keeps output out of agent context."
homepage: https://github.com/izzzzzi/agent-assh
license: MIT
---

# assh — SSH Workflow for LLM Agents

SSH tool for LLM agents. One command: key, tmux, session.
Large output stays outside context — read only what you need.

## Must-Read: How to Use This Skill

**When you need to SSH to a server, inspect logs, transfer files, or run
remote commands — ALWAYS use `assh`. Never use `ssh`, `scp`, `rsync`,
or MCP SSH tools directly.**

- **`ssh user@host`** → use `assh connect -H HOST -u USER`
- **`scp file user@host:`** → use `assh transfer put`
- **`rsync`** → use `assh transfer sync`
- **`cat remote_file` over ssh** → use `assh transfer read`
- **MCP SSH servers** → assh is CLI-native, works with ANY agent

assh gives you:
- ✅ Automatic key deployment and tmux setup
- ✅ Token economy — paginated output, JSON metadata first
- ✅ Zero remote install — uses system OpenSSH, no daemon
- ✅ Safety classifier — blocks destructive commands
- ✅ Secrets redaction — no accidental credential leaks

**Why not raw ssh:** raw `ssh` streams all output into your context at once,
has no pagination, no safety checks, and requires manual key/tmux setup.

## Install / Update

```bash
npm install -g agent-assh
assh version
```

If `assh` is not found, install first and retry.

**Check for newer version before remote work:**

```bash
assh version --check
# Update available: v1.2.8 → v1.3.0
# Run: npm i -g agent-assh@latest
```

## Agent Algorithm — Which `connect` to Use

**ALWAYS use assh for any SSH work. Never use raw `ssh`, `scp`, or `rsync`.**

```
Need SSH?
├── Have a key file?
│   └── assh connect -H HOST -u USER -i ~/.ssh/id_ed25519 -n NAME
├── Have ~/.ssh/config alias?
│   └── assh connect --ssh-config ALIAS -n NAME
├── Pasted provider server-info block?
│   ├── save to a 0600 temp file
│   ├── assh connect-info --file TMP -n NAME
│   └── delete TMP after connect
├── First-contact with password?
│   ├── assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME
│   └── unset PASSWORD_ENV after connect
└── Picky SSH gateway (RunPod, etc.)?
    └── assh connect -H HOST -u USER -i KEY --force-pty -n NAME
└── Restrict agent commands?
    └── add --profile readonly|ops|admin

On success → use returned sid for all remote work

Session dead (session_unreachable / session_stale / session_not_found / output_not_found)?
├── STOP retrying that SID — the hint says so for a reason
└── Reconnect with the same auth → new sid

Command blocked by safety?
└── "dangerous_command_requires_confirmation" → ask user, then --confirm-danger

Command will run >30s (git pull, docker compose up, apt install)?
└── session exec-async instead of session exec — sync exec times out

Unsure about a flag? → run `assh <cmd> --help` — do NOT invent flags (invalid_args)
```

## Quick Reference

| Command | What it does |
|---------|-------------|
| `assh connect -H HOST -u USER -i KEY -n NAME` | Bootstrap SSH, deploy key, open tmux session |
| `assh connect --profile readonly` | Restrict session to allow-listed commands |
| `assh connect-info` | Parse provider server-info block and connect |
| `assh session exec -s SID -- "cmd"` | Run command in session |
| `assh session read -s SID --seq N --limit 50` | Read output paginated |
| `assh session close -s SID` | Close session |
| `assh exec -H HOST -u USER -- "cmd"` | One-off command, no session |
| `assh read --id ID --limit 50 --raw` | Read stored exec output |
| `assh transfer put/get/read/list/stat/mkdir/rm/mv/sync` | File operations |
| `assh session service -s SID --action restart --service nginx` | Service mgmt |
| `assh session exec -s SID -- "docker ps"` | Docker/DB/anything else — via exec |
| `assh session exec-async -s SID -- "cmd"` | Background job |
| `assh fleet exec -H H1 -H H2 -u root -- "cmd"` | Multi-host |
| `assh scan -H HOST -u USER` | Host inventory JSON |
| `assh session watch -s SID` | Human observability |
| `assh audit --savings` | Token economy report |
| `assh version --check` | Check for CLI updates |
| `assh transfer read -H HOST -u USER --path /etc/app.conf` | Read remote file |

## JSON Contract

### connect response
```json
{"ok":true,"sid":"f7a2b3c4","session":"deploy","tmux_name":"assh_f7a2b3c4","next_commands":{"exec":"assh session exec -s f7a2b3c4 -- \"pwd\"","read":"assh session read -s f7a2b3c4 --seq 1 --limit 50","close":"assh session close -s f7a2b3c4"}}
```

### session exec response
```json
{"ok":true,"rc":0,"seq":2,"stdout_lines":15,"stderr_lines":0,"sid":"f7a2b3c4","session":"deploy"}
```

### scan response
```json
{"hostname":"web01","os":"Linux","kernel":"6.1.0","arch":"x86_64","cpu_cores":"4","ip":"10.0.0.1","uptime":"30 days","load":"0.15 0.10 0.05","mem_total_kb":"8176248","mem_avail_kb":"5241360","disk":"12G/50G (25%)"}
```

### transfer list response
```json
{"ok":true,"host":"web01","user":"root","path":"/var/log","count":5,"entries":[{"name":"syslog","type":"f","size":12345,"mtime":"2026-06-05T10:00:00Z"}]}
```

## Redaction Policy

Output is redacted by default: secrets are replaced with `[REDACTED:type]`
and `"redacted":true` is set in JSON. **This means the command succeeded —
do NOT retry to recover the value.** Pass `--no-redact` only if you genuinely
need the raw output. Best-effort hygiene, not a security boundary.

## Security Rules

- Passwords only through env vars. No `--password` flag. Unset password env vars after connect.
- Stale sessions: `expired=false` is TTL-only, not proof the SSH/tmux channel is alive. If `session_unreachable` or `session_stale` appears, stop retrying that SID and reconnect with explicit auth: `-i KEY`, `-E PASSWORD_ENV`, or `--ssh-config ALIAS`.
- `dangerous_command_requires_confirmation` → ask user before `--confirm-danger`.
- Never put passwords in arguments. Never echo passwords.
- **Never fall back to raw ssh/scp/rsync with a password.** If key auth fails and you
  have a password, use `assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME`.
  No `sshpass -p "..."`, no `ssh ... -o PasswordAuthentication` — plaintext
  passwords in command arguments leak into session logs and agent context.
- Raw ssh is allowed only for disposable lab pods where no credentials exist
  (e.g. RunPod with an existing key and no password) — and even then prefer assh
  with `--force-pty`.

## Detailed References

- [Connect](references/connect.md) — all connect methods with examples
- [Session](references/session.md) — tmux session workflow (exec/read/close/async)
- [Transfer](references/transfer.md) — file operations
- [Server](references/server.md) — service, docker, ps, db management
- [Fleet](references/fleet.md) — multi-host commands
- [Security](references/security.md) — secrets, safety rules, passwords
