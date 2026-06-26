## assh SSH Workflow

**When you need SSH — ALWAYS use `assh`. Never use raw `ssh`, `scp`, or `rsync`.**

The assh skill (`skills/assh/SKILL.md`, `AGENTS.md`, or CLI plugin) has the
complete decision tree and JSON contracts. If your runtime loaded it, follow it.

Otherwise, use the instructions below.

Use `assh` for SSH work so large remote output stays out of the agent context.

Install when missing:

```bash
npm i -g agent-assh
assh version
assh version --check  # check for updates
```

### Agent Algorithm

```text
Need SSH?
  Simplest path — direct host + key, no alias, no password:
    assh connect -H HOST -u root -i KEY -n NAME

  Alternative — ~/.ssh/config alias:
    assh connect --ssh-config ALIAS -n NAME

  Alternative — pasted provider server info:
    save to a 0600 temp file
    assh connect-info --file TMP -n NAME
    delete TMP after connect
    if parsing fails, extract host/user/password, put password in env, then use connect

  Alternative — first-contact with password:
    assh connect -H HOST -u root -E PASSWORD_ENV -n NAME

  Picky SSH gateways (RunPod, etc.) — add --force-pty:
    assh connect -H HOST -u root -i KEY --force-pty -n NAME
    assh exec -H HOST -u root -i KEY --force-pty -- "command"
    assh read --id OUTPUT_ID --raw               # read exec output

  Restrict commands? — add --profile:
    assh connect ... --profile readonly           # read-only commands only
    assh connect ... --profile ops                # + restarts, pulls
    assh connect ... --profile admin              # full access (default)

  Scan host health:
    assh scan -H HOST -u USER

  Continue with returned sid:
    assh session exec -s SID -- "pwd"
    assh session read -s SID --seq 1 --limit 50
    assh session exec -s SID --timeout 600 -- "git pull"
    assh session read -s SID --seq 2 --stream stderr --limit 50

  File operations:
    assh transfer list -H HOST -u USER --path /var/log
    assh transfer stat -H HOST -u USER --path /etc/nginx.conf
    assh transfer put -H HOST -u USER LOCAL_PATH REMOTE_PATH
    assh transfer get -H HOST -u USER REMOTE_PATH LOCAL_PATH
    assh transfer sync --direction push --source ./dist --dest /var/www -H HOST
    assh transfer mkdir -H HOST -u USER --path /opt/newapp
    assh transfer rm -H HOST -u USER --path /tmp/junk.log
    assh transfer mv -H HOST -u USER --source /tmp/a --dest /tmp/b
    assh transfer read -H HOST -u USER --path /etc/app.conf  # remote file -> output_id, then: assh read --id ID

  Server management:
    assh session ps -s SID --top 20
    assh session kill -s SID --pid 1234
    assh session service -s SID --action status --service nginx
    assh session service -s SID --action restart --service docker
    assh session service -s SID --action logs --service nginx --lines 100

  Background jobs:
    assh session exec-async -s SID -- "long-build.sh"
    assh session job-status -s SID --job-id JOB_ID
    assh session job-status -s SID --job-id JOB_ID --raw  # bare output, no JSON

  Docker:
    assh session docker-ps -s SID
    assh session docker-logs -s SID --container myapp
    assh session docker-exec -s SID --container myapp -- "ls -la"

  Database (read-only):
    assh session db-query -s SID --type mysql -d mydb -q "SELECT COUNT(*) FROM users"

  Fleet (multi-host):
    assh fleet exec -H host1 -H host2 -H host3 -u root -- "uptime"

  Pre/post hooks:
    assh session exec -s SID --before "git stash" --after "git stash pop" -- "deploy.sh"

  Watch agent session (human observability):
    assh session watch -s SID
    # Copy the attach_cmd to a terminal to see the agent's tmux in real-time.

  Cleanup:
    assh session gc --older-than 24h --execute

  Audit:
    assh audit --last 20 --host HOST --failed
```

### JSON Contract

`assh connect` returns a session id and next commands:

```json
{"ok":true,"sid":"f7a2b3c4","session":"deploy","tmux_name":"assh_f7a2b3c4","next_commands":{"exec":"assh session exec -s f7a2b3c4 -- \"pwd\"","read":"assh session read -s f7a2b3c4 --seq 1 --limit 50","close":"assh session close -s f7a2b3c4"}}
```

`assh session exec` returns command metadata:

```json
{"ok":true,"rc":0,"seq":2,"stdout_lines":15,"stderr_lines":0,"sid":"f7a2b3c4","session":"deploy"}
```

`assh scan` returns host inventory:

```json
{"hostname":"web01","os":"Linux","kernel":"6.1.0","arch":"x86_64","cpu_cores":"4","ip":"10.0.0.1","uptime":"30 days","load":"0.15 0.10 0.05","mem_total_kb":"8176248","mem_avail_kb":"5241360","disk":"12G/50G (25%)"}
```

`assh transfer list` returns file entries:

```json
{"ok":true,"host":"web01","user":"root","path":"/var/log","count":5,"entries":[{"name":"syslog","type":"f","size":12345,"mtime":"2026-06-05T10:00:00Z"}]}
```

## Token Economy

1. `assh session exec` → JSON metadata (always fits context)
2. `assh session read --raw` → clean text, no `\n` or JSON wrapper
3. `assh session read` (no `--raw`) → only when pagination needed
4. Always `--limit N` — don't read more than you need

Rules:

- Operational commands emit one JSON value by default.
- `read --raw` and `session read --raw` print only content (not JSON).
- Output is redacted by default: `[REDACTED:type]` and `"redacted":true` mean assh masked a secret; the command succeeded, do not retry to recover it. `--no-redact` disables it. Best-effort hygiene, not a security boundary.
- `assh audit --savings` summarizes output lines withheld by pagination (line metric, not tokens).
- Remote non-zero status is a command result, not a transport failure.
- Passwords are only accepted through environment variables; never put passwords in command arguments.
- Command text is not written to audit logs.
- If `session exec` returns `dangerous_command_requires_confirmation`, ask for explicit user intent before rerunning with `--confirm-danger`.
- db-query is read-only — only SELECT/SHOW/DESCRIBE/EXPLAIN allowed.
- session watch shows a tmux attach command; the human opens a terminal to observe the agent.
