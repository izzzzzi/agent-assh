# assh

[![CI](https://github.com/izzzzzi/agent-assh/actions/workflows/ci.yml/badge.svg)](https://github.com/izzzzzi/agent-assh/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/izzzzzi/agent-assh)](https://github.com/izzzzzi/agent-assh/releases)
[![npm](https://img.shields.io/npm/v/agent-assh)](https://www.npmjs.com/package/agent-assh)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

SSH workflow helper for LLM agents.

`assh` bootstraps SSH access, opens a persistent remote `tmux` session, and keeps large SSH output out of the agent context. Commands return compact JSON metadata first; agents read only the lines they need.

## Quick Start

```bash
npm i -g agent-assh

export TARGET_PASS="..."
assh connect -H 203.0.113.10 -u root -E TARGET_PASS -n deploy
unset TARGET_PASS
```

If key login already works, `assh connect` does not read `TARGET_PASS`.

```bash
assh connect -H 203.0.113.10 -u root -i ~/.ssh/id_agent_ed25519 -n deploy
```

`connect` returns a session id and `next_commands`:

```json
{
  "ok": true,
  "sid": "f7a2b3c4",
  "session": "deploy",
  "tmux_name": "assh_f7a2b3c4",
  "next_commands": {
    "exec": "assh session exec -s f7a2b3c4 -- \"pwd\"",
    "read": "assh session read -s f7a2b3c4 --seq 1 --limit 50",
    "close": "assh session close -s f7a2b3c4"
  }
}
```

Continue through the session API:

```bash
assh session exec -s f7a2b3c4 -- "pwd"
assh session read -s f7a2b3c4 --seq 1 --limit 50
assh session close -s f7a2b3c4
```

## What connect Does

`assh connect`:

- creates or reuses `~/.ssh/id_agent_ed25519` unless `--identity` is set;
- tries key login first;
- uses `--password-env` only when key login fails;
- deploys the public key and verifies key login;
- probes remote capabilities;
- installs `tmux` non-interactively unless `--no-install-tmux` is set;
- runs safe cleanup for old trusted `assh` sessions unless `--no-gc` is set;
- opens a trusted `tmux` session and saves local registry metadata.

## Commands

- `assh connect`: first-contact bootstrap and session open.
- `assh session exec|read|close|gc`: persistent tmux workflow.
- `assh exec`: run one remote command and store output locally.
- `assh read`: read stored output with pagination or `--raw`.
- `assh capabilities`: inspect remote session support.
- `assh scan`: return host inventory JSON.
- `assh key-deploy`: low-level key deployment using a password from env.
- `assh audit`: read local audit events with `--last`, `--host`, and `--failed`.
- `assh version`: print version metadata.

## Token Economy

Use metadata first, then read targeted output windows:

```bash
assh session exec -s f7a2b3c4 -- "journalctl -p warning"
assh session read -s f7a2b3c4 --seq 1 --limit 50
assh session read -s f7a2b3c4 --seq 1 --stream stderr --limit 50
```

Use `--raw` only for piping or exact output:

```bash
assh session read -s f7a2b3c4 --seq 1 --raw
```

## Security

- Passwords are accepted only through environment variables. There is no `--password` flag.
- If key login works, `connect` does not read the password env var.
- Password values are not written to audit logs.
- Command text is not written to audit logs; audit entries use command hashes.
- SSH runs non-interactively and disables pseudo-terminal allocation.
- `--host-key-policy accept-new` is the default. Use `strict` for hardened environments.
- `--host-key-policy no-check` is unsafe and should be limited to disposable lab/dev hosts.
- Remote cleanup only targets sessions with trusted `assh` metadata.

## Advantages

- One command handles first login, key setup, tmux readiness, cleanup, and session open.
- Large output stays outside the agent context until explicitly paged in.
- Persistent sessions preserve working directory and environment between commands.
- JSON responses are stable for agent parsing.

## Limitations

- `tmux` sessions are for Unix-like remotes.
- Package installation is non-interactive; unsupported package managers return machine-readable errors.
- Interactive password prompts are not supported in v1.
- `assh` uses the system OpenSSH client.

## Manual Install

`npm i -g agent-assh` installs a wrapper that downloads the matching Go binary from GitHub Releases. You can also download release archives manually from:

```text
https://github.com/izzzzzi/agent-assh/releases
```

## Russian

See [README.ru.md](README.ru.md).
