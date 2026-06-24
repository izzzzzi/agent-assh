# assh Session — Persistent tmux Workflow

After `connect` returns a `sid`, use these commands.

## Exec — Run a Command

```bash
assh session exec -s f7a2b3c4 -- "pwd"
# {"ok":true,"rc":0,"seq":1,"stdout_lines":1,"stderr_lines":0,"sid":"f7a2b3c4","session":"deploy"}
```

## Read — Get Output

Always use `--limit` to keep output small:

```bash
assh session read -s f7a2b3c4 --seq 1 --limit 50
assh session read -s f7a2b3c4 --seq 1 --stream stderr --limit 50
```

Use `--raw` only for piping or exact output.

## Timeout for long commands

```bash
assh session exec -s f7a2b3c4 --timeout 600 -- "git pull"
```

## Pre/post hooks

```bash
assh session exec -s f7a2b3c4 --before "git stash" --after "git stash pop" -- "deploy.sh"
```

## Close

```bash
assh session close -s f7a2b3c4
```

## One-Off Commands (no tmux)

```bash
assh exec -H HOST -u root -i KEY --force-pty -- "curl -s localhost:8188"
# {"ok":true,"output_id":"ABC123","stdout_lines":3,"stderr_lines":0}
assh read --id ABC123 --raw
```

## Background Jobs

```bash
assh session exec-async -s f7a2b3c4 -- "make build"
# Returns job_id
assh session job-status -s f7a2b3c4 --job-id JOB_ID
assh session job-status -s f7a2b3c4 --job-id JOB_ID --raw
assh session job-cancel -s f7a2b3c4 --job-id JOB_ID
```

## Process Management

```bash
assh session ps -s f7a2b3c4 --top 20
assh session ps -s f7a2b3c4 --filter nginx
assh session kill -s f7a2b3c4 --pid 1234
assh session kill -s f7a2b3c4 --pid 1234 --signal KILL
```

## Session Cleanup

```bash
assh session gc --older-than 24h --execute
```

## Context Discipline — Token Economy

**First, check metadata (always fits in context):**

```bash
assh session exec -s SID -- "command"
# Returns {"stdout_lines":N,"stderr_lines":M} — tiny JSON
```

**Then read only what you need:**

| Goal | Use | Why |
|------|-----|-----|
| View output | `session read --raw` | Clean text, no `\n`, fewer tokens |
| Parse JSON | `session read` | Has `has_more`, `total_lines` |
| Large output | `--limit N` | Only N lines into context |
| Stderr only | `--stream stderr` | Skip stdout |

**Rule:**
- `exec` → always JSON (metadata, few tokens)
- `read --raw` → for reading output (human or agent)
- `read` (no `--raw`) → only when pagination needed (`has_more`, `total_lines`)
- `audit --savings` → shows lines withheld from context

## Errors

| Error | Meaning | Fix |
|-------|---------|-----|
| `session_not_found` | sid invalid or expired | Reconnect |
| `dangerous_command_requires_confirmation` | Destructive command blocked | Ask user before `--confirm-danger` |
| Non-zero `rc` | Remote command failed | Check stderr for the actual error |
