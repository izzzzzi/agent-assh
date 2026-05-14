## assh SSH Workflow

Use `assh` for SSH work so large remote output stays out of the agent context.

Install when missing:

```bash
npm i -g agent-assh
assh version
```

### Agent Algorithm

```text
Need SSH?
  One command:
    assh exec -H HOST -u root -i KEY -- "cmd"
    inspect stdout_lines/stderr_lines
    assh read --id OUTPUT_ID --limit 20 --offset 0
    assh read --id OUTPUT_ID --stream stderr --raw

  Related commands with shared directory/env:
    assh session open -H HOST -u root -i KEY -n NAME
    assh session exec -s SID -- "cd /app"
    assh session exec -s SID --timeout 600 -- "git pull"
    assh session read -s SID --seq 2 --limit 20
    assh session read -s SID --seq 2 --raw
    assh session close -s SID

  Cleanup:
    assh session gc --older-than 24h --execute

  Audit:
    assh audit --last 20 --host HOST --failed
```

### JSON Contract

`assh exec` returns metadata and stores output:

```json
{"ok":true,"exit_code":0,"output_id":"a1b2c3d4","stdout_lines":4327,"stderr_lines":0}
```

`assh read` returns paginated content unless `--raw` is used:

```json
{"ok":true,"output_id":"a1b2c3d4","stream":"stdout","offset":0,"limit":20,"total_lines":4327,"has_more":true,"content":"..."}
```

`assh session exec` returns command metadata:

```json
{"ok":true,"rc":0,"seq":2,"stdout_lines":15,"stderr_lines":0,"sid":"f7a2b3c4","session":"deploy"}
```

Rules:

- Operational commands emit one JSON value by default.
- `read --raw` and `session read --raw` print only content.
- Remote non-zero status is a command result, not a transport failure.
- There is no `cwd` response field.
- There is no `attempt` response field.

### Errors

```json
{"ok":false,"error":"auth_failed"}
{"ok":false,"error":"host_key_failed"}
{"ok":false,"error":"connection_error"}
{"ok":false,"error":"timeout"}
{"ok":false,"error":"tmux_missing"}
```

Passwords are only accepted through environment variables for `key-deploy`; never put passwords in command arguments. Command text is not written to audit logs.
