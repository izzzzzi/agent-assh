# assh Agent Instructions

`assh` is the SSH workflow helper for LLM agents. It returns compact JSON metadata for remote commands and stores large output locally so the agent can read only the lines it needs.

## Install

```bash
npm i -g agent-assh
assh version
```

## Decision Flow

```text
Need SSH?
  One command:
    assh exec -> read stdout_lines/stderr_lines -> assh read
  Related commands that share directory or environment:
    assh session open -> session exec -> session read -> session close
  Host inventory:
    assh scan
  First login with password:
    assh key-deploy with password from env
```

## Commands

### exec

```bash
assh exec -H HOST -u root -i ~/.ssh/id_ed25519 -- "df -h"
```

Example response:

```json
{"ok":true,"exit_code":0,"output_id":"a1b2c3d4","stdout_lines":10,"stderr_lines":0}
```

Remote non-zero exit status is a command result. It is not a transport failure.

### read

```bash
assh read --id a1b2c3d4 --limit 10 --offset 0
assh read --id a1b2c3d4 --stream stderr --limit 20
assh read --id a1b2c3d4 --raw
```

Use `read --raw` when piping output to local tools. Raw mode prints only content, without JSON.

### session

```bash
assh session open -H HOST -u root -i ~/.ssh/id_ed25519 -n deploy
assh session exec -s SID -- "cd /app"
assh session exec -s SID --timeout 600 -- "git pull"
assh session read -s SID --seq 2 --limit 20
assh session read -s SID --seq 2 --stream stderr --raw
assh session close -s SID
```

Example `session exec` response:

```json
{"ok":true,"rc":0,"seq":2,"stdout_lines":15,"stderr_lines":0,"sid":"f7a2b3c4","session":"deploy"}
```

Use `session read --raw` when piping session output to local tools.

Clean up old trusted `assh` tmux sessions:

```bash
assh session gc --older-than 24h --execute
```

### scan

```bash
assh scan -H HOST -u root -i ~/.ssh/id_ed25519
```

### key-deploy

```bash
export TARGET_PASS="secret"
assh key-deploy -H HOST -u root -E TARGET_PASS
unset TARGET_PASS
```

Passwords must come from environment variables. Never put a password in command arguments.

### audit

```bash
assh audit --last 20 --host HOST --failed
```

## JSON Rules

- Operational commands emit one JSON value by default.
- Errors use `{"ok":false,"error":"code","message":"..."}`.
- `exec` responses include `exit_code`, `output_id`, `stdout_lines`, and `stderr_lines`.
- `session exec` responses include `rc`, `seq`, `stdout_lines`, `stderr_lines`, `sid`, and `session`.
- There is no `cwd` response field.
- There is no `attempt` response field.

## Error Handling

```json
{"ok":false,"error":"auth_failed"}
{"ok":false,"error":"host_key_failed"}
{"ok":false,"error":"connection_error"}
{"ok":false,"error":"timeout"}
{"ok":false,"error":"tmux_missing"}
```

On `auth_failed`, ask the user for valid credentials. Do not retry with guessed passwords.

## Context Discipline

If `stdout_lines` or `stderr_lines` is large, do not read all output. Read targeted windows with `--limit`, `--offset`, and `--stream`; use raw mode only when another local command consumes the output.
