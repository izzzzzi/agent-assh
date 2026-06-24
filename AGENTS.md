Use `assh` for SSH work so large remote output stays out of the agent context.

Install when missing:
```bash
npm i -g agent-assh
assh version
```

### Agent Algorithm for SSH

```
Need SSH?
├── Have a key file?        → assh connect -H HOST -u USER -i KEY -n NAME
├── Have SSH config alias?  → assh connect --ssh-config ALIAS -n NAME
├── Pasted provider block?  → save to 0600 temp → assh connect-info --file TMP -n NAME → rm
├── First-contact w/ pass?  → assh connect -H HOST -u USER -E PASS_ENV -n NAME
└── Picky gateway?          → assh connect ... --force-pty -n NAME
```

### Quick Reference

| Command | What |
|---------|------|
| `assh session exec -s SID -- "cmd"` | Run command in tmux session |
| `assh session read -s SID --seq N --limit 50` | Read paginated output |
| `assh session close -s SID` | Close session |
| `assh exec -H HOST -u USER -- "cmd"` | One-off command, no tmux |
| `assh read --id ID --raw` | Read stored exec output |
| `assh transfer put/get/read/list/stat/mkdir/rm/mv/sync` | File operations |
| `assh session service -s SID --action restart --service NAME` | Service mgmt |
| `assh session docker-ps/docker-logs/docker-exec -s SID` | Docker |
| `assh session db-query -s SID --type mysql -d DB -q "SELECT"` | Read-only DB |
| `assh session exec-async -s SID -- "cmd"` | Background job |
| `assh fleet exec -H H1 -H H2 -u root -- "cmd"` | Multi-host |
| `assh scan -H HOST -u USER` | Host inventory JSON |
| `assh transfer read -H HOST -u USER --path FILE` | Read remote file |

### JSON Contract

`connect` → `{"ok":true,"sid":"...","next_commands":{"exec":"...","read":"...","close":"..."}}`
`session exec` → `{"ok":true,"rc":0,"seq":N,"stdout_lines":N,"stderr_lines":N,"sid":"..."}`
`scan` → JSON with hostname, OS, CPU, disk, memory
`transfer list` → `{"ok":true,"entries":[{"name":"...","type":"f|d","size":N}]}`

### Security Rules

- Passwords only through env vars. No `--password` flag.
- `[REDACTED:type]` + `"redacted":true` = command succeeded, do not retry
- `dangerous_command_requires_confirmation` → ask user before `--confirm-danger`
- `db-query` is read-only, `session exec` blocks rm -rf/mkfs/etc
- Never put passwords in arguments. Never echo passwords.
- `transfer read` errors: `remote_file_not_found`, `not_a_file`, `file_too_large`, `binary_file`, `permission_denied`
- `assh audit --savings` shows lines withheld by pagination (line metric)
