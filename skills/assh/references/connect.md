# assh connect

## Methods

```bash
# key file
assh connect -H 203.0.113.10 -u root -i ~/.ssh/id_ed25519 -n deploy

# ~/.ssh/config alias (host, user, port and key come from the config)
assh connect --ssh-config my-alias -n deploy

# password, first contact: assh installs its own key, later logins use it
assh connect -H 203.0.113.10 -u root -E TARGET_PASS -n deploy
unset TARGET_PASS
```

`-E` names an environment variable that already holds the password. If key
login works, the variable is not read.

### Pasted provider block

`connect-info` reads `Label: value` lines (host/IPv4, user/login, password,
port; English or Russian labels). Create the file with your file-writing tool,
or under `umask 077`, so the password never sits in a world-readable file:

```bash
( umask 077; cat > /tmp/server.txt )   # paste the block, then Ctrl-D
assh connect-info --file /tmp/server.txt -n deploy
rm -f /tmp/server.txt
```

`connect-info` has no `-E`: the password comes from the block. If parsing
fails, use the password method above.

## Options

| Flag | Use |
| --- | --- |
| `-p PORT`, `-J JUMP` | non-default port, jump host |
| `--reuse` | return a live session for the same host, user, key and profile instead of opening a new one |
| `--force-pty` | gateways that reject `-T` (RunPod); stderr is merged into stdout in this mode |
| `--profile NAME` | restrict the session, see below |
| `--no-install-tmux` | fail with `tmux_missing` instead of installing tmux |
| `--ttl 12h` | session lifetime in the local registry |
| `--host-key-policy` | `accept-new` (default), `strict`, `no-check` |

## Response

`ok`, `sid`, `session` (your `-n` label), `tmux_name`, `host`, `user`,
`identity` (the key to pass as `-i` to `transfer` and `exec`), `key_deployed`,
`reused` (only with `--reuse`), `next_commands` (`exec`, `read`, `list`,
`close` with the sid filled in).

## Profiles

`--profile readonly` allows log and status reads; `ops` adds restarts,
`git pull` and `apt update`; `admin` (default) allows everything. The lists
live in `~/.config/assh/profiles.json`.

A profile is checked by `session exec`, `session exec-async`,
`session service` and `session kill`. Every part of a compound command must be
allowed, and command substitution or a redirect into a file is refused:

```json
{"ok":false,"error":"command_not_allowed","message":"'touch /tmp/x' is not in profile 'readonly'","hint":"use a different profile or connect without --profile"}
```

`transfer`, `exec` and `fleet` address a host, not a session, so no profile
applies to them.

## Errors

| Error | Meaning | Fix |
| --- | --- | --- |
| `auth_failed` | key or password rejected | check credentials; with a password use `-E` |
| `tmux_missing` | tmux is not installed or not on the non-interactive `PATH` | drop `--no-install-tmux`, or install tmux on the host |
| `host_key_failed` | the host key changed since it was first seen | stop and tell the user; do not switch to `no-check` on your own |
| `connection_error`, `timeout` | network problem | check host, port, reachability |
