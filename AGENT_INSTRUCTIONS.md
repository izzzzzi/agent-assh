# assh Agent Instructions

`assh` is the SSH workflow helper for LLM agents. Start with `assh connect` so access, key setup, tmux, cleanup, and the first persistent session are prepared in one step.

## Install

```bash
npm i -g agent-assh
assh version
```

## First SSH Step

Pick the right connect method — assh works with ANY combination of host, key, password, and SSH config alias.

**1. Direct host + key** (most common for agents):

```bash
assh connect -H HOST -u root -i ~/.ssh/id_ed25519 -n NAME
```

`-H` is a raw hostname/IP — no `~/.ssh/config` alias, no password needed.

**2. SSH config alias** (when host is in `~/.ssh/config`):

```bash
assh connect --ssh-config my-alias -n NAME
```

**3. Pasted server-info block** (provider credentials):

```bash
assh connect-info --file /path/to/tmp-server-info -n NAME
```

Then delete the temp file. If parsing fails, extract host/user/password, put password in env, and use `assh connect -E PASSWORD_ENV`.

**4. First-contact with password only:**

```bash
assh connect -H HOST -u root -E PASSWORD_ENV -n NAME
```

The key point: `-H` + `-i` is the simplest path. No `~/.ssh/config`, no password, no alias. Just host and key file.

**Picky SSH gateways (RunPod, etc.):** if connect fails with `unsupported remote session backend` or `Your SSH client doesn't support PTY`, add `--force-pty`:

```bash
assh connect -H HOST -u USER -i KEY --force-pty -n NAME
```

This uses `-tt` instead of `-T` and pipes commands via stdin. Also works with `assh exec`:

```bash
assh exec -H HOST -u USER -i KEY --force-pty -- "command"
```

Use the returned `sid` and `next_commands` for all remote work.

## Normal Workflow

```bash
assh session exec -s SID -- "pwd"
assh session read -s SID --seq 1 --limit 50
assh session exec -s SID --timeout 600 -- "git pull"
assh session read -s SID --seq 2 --stream stderr --limit 50
assh session close -s SID
```

Pre/post hooks:

```bash
assh session exec -s SID --before "git stash" --after "git stash pop" -- "deploy.sh"
```

Clean up stale trusted sessions:

```bash
assh session gc --older-than 24h --execute
```

## One-Off Commands

Use `exec/read` when no persistent directory or environment is needed:

```bash
assh exec -H HOST -u root -i KEY -- "df -h"
assh read --id OUTPUT_ID --limit 50
```

## Host Scanning

```bash
assh scan -H HOST -u USER
# Returns JSON with hostname, OS, kernel, arch, CPU cores, IP, uptime, load, memory, disk
```

## File Operations

```bash
assh transfer list -H HOST -u USER --path /var/log
assh transfer stat -H HOST -u USER --path /etc/nginx.conf
assh transfer put -H HOST -u USER LOCAL_PATH REMOTE_PATH
assh transfer get -H HOST -u USER REMOTE_PATH LOCAL_PATH
assh transfer sync --direction push --source ./dist --dest /var/www -H HOST -u USER
assh transfer sync --direction pull --source /var/log --dest ./logs -H HOST -u USER
assh transfer mkdir -H HOST -u USER --path /opt/newapp
assh transfer rm -H HOST -u USER --path /tmp/junk.log
assh transfer rm -H HOST -u USER --path /tmp/old --recursive
assh transfer mv -H HOST -u USER --source /tmp/a --dest /tmp/b
```

## Process Management

```bash
assh session ps -s SID --top 20
assh session ps -s SID --filter nginx
assh session kill -s SID --pid 1234
assh session kill -s SID --pid 1234 --signal KILL
```

## Service Management

```bash
assh session service -s SID --action status --service nginx
assh session service -s SID --action restart --service docker
assh session service -s SID --action start --service postgresql
assh session service -s SID --action stop --service apache2
assh session service -s SID --action logs --service nginx --lines 100
```

## Background Jobs

For long-running commands (builds, deployments):

```bash
assh session exec-async -s SID -- "make build"
# Returns job_id for tracking
assh session job-status -s SID --job-id JOB_ID
assh session job-cancel -s SID --job-id JOB_ID
```

## Docker Management

```bash
assh session docker-ps -s SID
assh session docker-ps -s SID -a  # all containers including stopped
assh session docker-logs -s SID --container myapp --tail 100
assh session docker-exec -s SID --container myapp -- "ls -la /app"
```

## Database Query (Read-Only)

Only SELECT, SHOW, DESCRIBE, and EXPLAIN queries are allowed for safety:

```bash
assh session db-query -s SID --type mysql -d mydb -q "SELECT COUNT(*) FROM users"
assh session db-query -s SID --type postgres -d mydb -q "SELECT * FROM orders LIMIT 10"
assh session db-query -s SID --type mysql -d mydb -U dbuser -W dbpass -q "SHOW TABLES"
```

## Fleet (Multi-Host)

Execute the same command across multiple hosts in parallel:

```bash
assh fleet exec -H host1 -H host2 -H host3 -u root -- "uptime"
assh fleet exec -H web01 -H web02 -u deploy -i ~/.ssh/id_ed25519 -- "df -h"
```

## Session Observability

Watch what the agent is doing in real-time:

```bash
assh session watch -s SID
# Returns an attach_cmd — paste it in a terminal to attach to the agent's tmux
```

## MCP Server (for Claude Code, Cursor, Windsurf)

Start as an MCP stdio server so agents can call assh as MCP tools:

```bash
assh mcp serve
# Wire into Claude Code: claude mcp add assh -- assh mcp serve
```

## JSON Rules

- Operational commands emit one JSON value by default.
- Errors use `{"ok":false,"error":"code","message":"..."}`.
- `session exec` responses include `rc`, `seq`, `stdout_lines`, `stderr_lines`, `sid`, and `session`.
- Remote non-zero exit status is a command result, not a transport failure.

## Context Discipline

If `stdout_lines` or `stderr_lines` is large, do not read all output. Use targeted windows with `--limit`, `--offset`, and `--stream`. Use `read --raw` or `session read --raw` only when piping or exact output is required.

## Security Rules

- Never put passwords in command arguments.
- Passwords are passed only through env vars named by `--password-env`.
- For pasted server-info blocks, prefer `connect-info --file`; remove the temporary file after connect.
- If key login works, `connect` does not read the password env var.
- Prefer `--host-key-policy strict` when host keys are already managed.
- Treat `--host-key-policy no-check` as unsafe and only for disposable lab/dev hosts.
- If `session exec` returns `dangerous_command_requires_confirmation`, do not add `--confirm-danger` unless the user explicitly intended the destructive action.
- `db-query` is read-only by design — write operations are blocked with a safety error.
- Use `assh session watch` to observe agent actions in real-time.

## Connection Pooling (Automatic)

`assh` uses SSH ControlMaster to multiplex connections to the same host. The first `connect` or `session exec` opens a master connection; subsequent commands reuse it. Control sockets live in `~/.ssh/controlmasters/` with a 5-minute idle timeout. No configuration needed — this is automatic.
