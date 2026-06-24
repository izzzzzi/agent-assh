# assh Connect — All Methods

## 1. Direct host + key (simplest)

```bash
assh connect -H 203.0.113.10 -u root -i ~/.ssh/id_ed25519 -n deploy
```

Returns `sid` and `next_commands`. Use the sid for all session work.

## 2. SSH config alias

```bash
assh connect --ssh-config my-server-alias -n deploy
```

## 3. Pasted provider server-info block

Save the exact block to a temp file with mode 0600:

```bash
cat > /tmp/server.txt << 'EOF'
Host: 203.0.113.10
User: root
Password: s3cr3t
EOF
chmod 0600 /tmp/server.txt
assh connect-info --file /tmp/server.txt -n deploy
rm -f /tmp/server.txt
```

If parsing fails, extract host/user/password manually and use method 4.

## 4. First-contact with password

```bash
export TARGET_PASS="s3cr3t"
assh connect -H 203.0.113.10 -u root -E TARGET_PASS -n deploy
unset TARGET_PASS
```

Password goes in env var, never in command arguments. If key login works,
`connect` does not read the password.

## 5. Picky SSH gateway (RunPod, etc.)

Add `--force-pty` for hosts that reject `-T`:

```bash
assh connect -H 203.0.113.10 -u root -i ~/.ssh/id_ed25519 --force-pty -n deploy
```

## 6. With command profile (restrict agent commands)

```bash
assh connect -H 203.0.113.10 -u root -i ~/.ssh/id_ed25519 -n deploy --profile readonly
```

Profiles limit what commands the session can run. Available profiles:
- `readonly` — log inspection, status checks, file reads
- `ops` — readonly + restarts, pulls, apt updates
- `admin` — full access (default)

Blocked command returns:

```json
{"ok":false,"error":"command_not_allowed","message":"'apt install nginx' is not in profile 'readonly'","hint":"use a different profile or connect without --profile"}
```

Profiles are defined in `~/.config/assh/profiles.json`.

## connect response fields

| Field | Type | Description |
|-------|------|-------------|
| `ok` | bool | true if session opened |
| `sid` | string | Session ID for all subsequent commands |
| `session` | string | Human-readable session name |
| `tmux_name` | string | Remote tmux session name |
| `next_commands` | object | exec/read/close templates with sid filled |

## Errors

| Error | Meaning | Fix |
|-------|---------|-----|
| `authentication_failed` | Bad key or password | Check credentials |
| `unsupported_remote_session_backend` | No tmux or PTY issue | Add `--force-pty` |
| `host_key_changed` | Host key mismatch | Use `--host-key-policy strict` or verify |
