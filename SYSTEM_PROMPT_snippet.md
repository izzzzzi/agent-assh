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
  First step:
    assh connect -H HOST -u root -E PASSWORD_ENV -n NAME
    or, when key login already works:
    assh connect -H HOST -u root -i KEY -n NAME

  Continue with returned sid:
    assh session exec -s SID -- "pwd"
    assh session read -s SID --seq 1 --limit 50
    assh session exec -s SID --timeout 600 -- "git pull"
    assh session read -s SID --seq 2 --stream stderr --limit 50
    assh session close -s SID

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

Rules:

- Operational commands emit one JSON value by default.
- `read --raw` and `session read --raw` print only content.
- Remote non-zero status is a command result, not a transport failure.
- Passwords are only accepted through environment variables; never put passwords in command arguments.
- Command text is not written to audit logs.
