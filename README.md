# assh

[![CI](https://github.com/agent-ssh/assh/actions/workflows/ci.yml/badge.svg)](https://github.com/agent-ssh/assh/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/agent-ssh/assh)](https://github.com/agent-ssh/assh/releases)
[![npm](https://img.shields.io/npm/v/agent-assh)](https://www.npmjs.com/package/agent-assh)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

SSH workflow helper for LLM agents.

`assh` keeps large SSH output out of the agent context. Commands return metadata first, and agents read only the lines they need. Persistent sessions use remote `tmux` so working directory and environment changes survive between related commands without adding `cwd` to command responses.

## Install

```bash
npm i -g agent-assh
assh version
```

GitHub Release archives are available for Linux, macOS, and Windows on amd64/arm64 where supported.

## Quick Start

```bash
assh exec -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -- "journalctl -p warning"
assh read --id OUTPUT_ID --limit 20 --offset 0
assh read --id OUTPUT_ID --stream stderr --raw
```

## Persistent Session

```bash
assh session open -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -n deploy
assh session exec -s SID -- "cd /app"
assh session exec -s SID --timeout 600 -- "git pull"
assh session read -s SID --seq 2 --limit 20
assh session read -s SID --seq 2 --stream stderr --raw
assh session gc --older-than 24h --execute
assh session close -s SID
```

## Commands

- `assh exec`: run one remote command and store output locally.
- `assh read`: read stored output with pagination or `--raw`.
- `assh session open|exec|read|close|gc`: persistent tmux workflow. `session exec` accepts `--timeout`.
- `assh capabilities`: inspect remote session support.
- `assh scan`: return host inventory JSON.
- `assh key-deploy`: deploy an SSH key using a password from env.
- `assh audit`: read local audit events with `--last`, `--host`, and `--failed`.
- `assh version`: print version metadata.

## JSON Contract

Operational commands emit one JSON value by default. `read --raw` and `session read --raw` print only stored content.

```json
{"ok":true,"exit_code":0,"output_id":"a1b2c3d4","stdout_lines":4327,"stderr_lines":0}
```

```json
{"ok":true,"output_id":"a1b2c3d4","stream":"stdout","offset":0,"limit":20,"total_lines":4327,"has_more":true,"content":"..."}
```

```json
{"ok":true,"rc":0,"seq":2,"stdout_lines":15,"stderr_lines":0,"sid":"f7a2b3c4","session":"deploy"}
```

Errors use:

```json
{"ok":false,"error":"tmux_missing","message":"tmux is not installed"}
```

`exec` and `session exec` treat remote non-zero status as command results, not transport failures. Response objects do not include `cwd` or `attempt` fields.

## Agent Workflow

```bash
assh audit --last 20 --host HOST --failed
```

Use `read --raw` for pipes and exact remote output. Use JSON mode when the agent needs pagination metadata.

## Security

- Passwords are accepted only through environment variables for `key-deploy`.
- Command text is not written to audit logs; audit entries use command hashes.
- Remote cleanup only targets sessions with trusted `assh` metadata.

## Russian

See [README.ru.md](README.ru.md).
