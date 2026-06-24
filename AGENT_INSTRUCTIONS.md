# assh Agent Instructions

`assh` is the SSH workflow helper for LLM agents.

**If your agent loaded the assh skill (via plugin, `AGENTS.md`, or `skills/assh/SKILL.md`):**
Follow the skill's instructions — it contains the complete decision tree and workflow.

**Otherwise,** use this document as the fallback guide.

Start with `assh connect` so access, key setup, tmux, cleanup, and the first persistent session are prepared in one step.

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

## One-Off Commands (no tmux session needed)

Use `exec` + `read` for quick commands without a persistent session. Best for PTY-gated hosts (RunPod).

```bash
# Run a command, capture output_id
assh exec -H HOST -u root -i KEY --force-pty -- "curl -s localhost:8188"
# Returns {"ok":true,"output_id":"ABC123","stdout_lines":3,"stderr_lines":0}

# Read the actual output using the output_id
assh read --id ABC123 --raw
# Prints clean output without JSON wrapper

# Paginated read with stream filtering
assh read --id ABC123 --limit 50 --stream stdout
```

Only one flag to remember: `--force-pty` for hosts that reject `-T` (RunPod, etc.).

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

Read a remote text file over ssh (cleaner and cheaper than `cat`; returns an
`output_id`, then page it with `assh read`):

```bash
assh transfer read -H HOST -u USER --path /etc/app.conf
# {"ok":true,"output_id":"ABC123","stdout_lines":42,"redacted":true,...}
assh read --id ABC123 --limit 50
```

`transfer read` refuses directories, oversized files (`--max-bytes`, default 1 MiB),
and binary files with typed errors (`remote_file_not_found`, `not_a_file`,
`file_too_large`, `binary_file`, `permission_denied`) and a `hint`. Use
`assh transfer get` to download binaries.

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
assh session job-status -s SID --job-id JOB_ID --raw  # bare content, no JSON wrapper
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

## JSON Rules

- Operational commands emit one JSON value by default.
- Errors use `{"ok":false,"error":"code","message":"..."}`.
- `session exec` responses include `rc`, `seq`, `stdout_lines`, `stderr_lines`, `sid`, and `session`.
- Remote non-zero exit status is a command result, not a transport failure.

## Context Discipline — Token Economy

1. **`exec` first** — always JSON metadata (fits context)
2. **`read --raw`** — clean text, no `\n` or JSON wrapper (fewer tokens)
3. **`read`** (no `--raw`) — only when pagination needed (`has_more`, `total_lines`)
4. **`--limit`** — always limit lines, don't read everything
5. **`audit --savings`** — shows lines withheld from context (line metric)

## Output Redaction

By default assh masks obvious secrets (AWS keys, JWTs, bearer tokens, PEM private
keys, `password=`/`token=` assignments) in stored and served output, replacing them
with `[REDACTED:type]`. When a response has `"redacted":true`, the masking is
intentional and the command itself succeeded — do NOT retry to recover the value.
Redaction is best-effort hygiene, not a security boundary. Pass `--no-redact` only
if you genuinely need the raw value.

## Security Rules

- Never put passwords in command arguments.
- Passwords are passed only through env vars named by `--password-env`.
- For pasted server-info blocks, prefer `connect-info --file`; remove the temporary file after connect.
- If key login works, `connect` does not read the password env var.
- Prefer `--host-key-policy strict` when host keys are already managed.
- Treat `--host-key-policy no-check` as unsafe and only for disposable lab/dev hosts.
- If `session exec` returns `dangerous_command_requires_confirmation`, do not add `--confirm-danger` unless the user explicitly intended the destructive action.
- `db-query` is read-only by design — write operations are blocked with a safety error.
- Operators may add deny-only rules in `~/.config/assh/safety.rules` (one command name per line). It can only ADD blocked commands, never relax built-in rules. The file must be mode `0600`; an invalid file fails closed with `safety_policy_invalid`.
- Use `assh session watch` to observe agent actions in real-time.

## Connection Pooling (Automatic)

`assh` uses SSH ControlMaster to multiplex connections to the same host. The first `connect` or `session exec` opens a master connection; subsequent commands reuse it. Control sockets live in `~/.ssh/controlmasters/` with a 5-minute idle timeout. No configuration needed — this is automatic.
