# assh Agent Instructions

`assh` is the SSH workflow helper for LLM agents. Start with `assh connect` so access, key setup, tmux, cleanup, and the first persistent session are prepared in one step.

## Install

```bash
npm i -g agent-assh
assh version
```

## First SSH Step

If the user pasted a provider server-info block, write the block to a temporary file with mode `0600`, run:

```bash
assh connect-info --file /path/to/tmp-server-info -n NAME
```

Then delete the temporary file. Server-info formats vary; if parsing fails, extract host, user, and password yourself, put the password in an environment variable, and use `assh connect -E PASSWORD_ENV`.

If first-contact password access may be needed:

```bash
assh connect -H HOST -u root -E PASSWORD_ENV -n NAME
```

If key login is already configured:

```bash
assh connect -H HOST -u root -i ~/.ssh/id_agent_ed25519 -n NAME
```

Use the returned `sid` and `next_commands`.

## Normal Workflow

```bash
assh session exec -s SID -- "pwd"
assh session read -s SID --seq 1 --limit 50
assh session exec -s SID --timeout 600 -- "git pull"
assh session read -s SID --seq 2 --stream stderr --limit 50
assh session close -s SID
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
