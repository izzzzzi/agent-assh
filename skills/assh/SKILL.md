---
name: assh
description: "Use assh for SSH — bootstraps connections, manages tmux sessions, keeps output out of agent context. Connecting, deploying, remote commands."
homepage: https://github.com/izzzzzi/agent-assh
license: MIT
---

# assh — SSH Workflow for LLM Agents

SSH-инструмент для LLM-агентов. Одна команда готовит ключ, tmux, сессию.
Большой вывод остаётся вне контекста — читайте только нужные строки.

## Install / Update

```bash
npm install -g agent-assh
assh version
```

If `assh` is not found, install first and retry.

## Agent Algorithm — Which `connect` to Use

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
│   └── assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME
└── Picky SSH gateway (RunPod, etc.)?
    └── assh connect -H HOST -u USER -i KEY --force-pty -n NAME

On success → use returned sid for all remote work
```

## Quick Reference

| Command | What it does |
|---------|-------------|
| `assh connect` | Bootstrap SSH, deploy key, open tmux session |
| `assh connect-info` | Parse provider server-info block and connect |
| `assh session exec -s SID -- "cmd"` | Run command in session |
| `assh session read -s SID --seq N --limit 50` | Read output paginated |
| `assh session close -s SID` | Close session |
| `assh exec -H HOST -u USER -- "cmd"` | One-off command, no session |
| `assh read --id ID --raw` | Read stored exec output |
| `assh transfer put/get/read/list/stat/mkdir/rm/mv/sync` | File operations |
| `assh session service -s SID --action restart --service nginx` | Service mgmt |
| `assh session docker-ps/docker-logs/docker-exec -s SID` | Docker |
| `assh session db-query -s SID --type mysql -d DB -q "SQL"` | Read-only DB |
| `assh session exec-async -s SID -- "cmd"` | Background job |
| `assh fleet exec -H H1 -H H2 -u root -- "cmd"` | Multi-host |
| `assh scan -H HOST -u USER` | Host inventory JSON |
| `assh session watch -s SID` | Human observability |
| `assh audit --savings` | Token economy report |
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

## Detailed References

- [Connect](references/connect.md) — all connect methods with examples
- [Session](references/session.md) — tmux session workflow (exec/read/close/async)
- [Transfer](references/transfer.md) — file operations
- [Server](references/server.md) — service, docker, ps, db management
- [Fleet](references/fleet.md) — multi-host commands
- [Security](references/security.md) — secrets, safety rules, passwords
