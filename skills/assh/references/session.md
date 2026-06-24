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

## Context Discipline

If `stdout_lines` or `stderr_lines` is large, do NOT read all output.
Use targeted windows with `--limit`, `--offset`, and `--stream`.
Use `read --raw` or `session read --raw` only when piping or exact output
is required.

## Errors

| Error | Meaning | Fix |
|-------|---------|-----|
| `session_not_found` | sid invalid or expired | Reconnect |
| `dangerous_command_requires_confirmation` | Destructive command blocked | Ask user before `--confirm-danger` |
| Non-zero `rc` | Remote command failed | Check stderr for the actual error |
