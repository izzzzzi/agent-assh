# assh security rules

## Passwords

- A password reaches assh only through an environment variable named by `-E`
  (`connect`, `key-deploy`) or inside a server-info file read by
  `connect-info`. There is no `--password` flag.
- Never put a password in a command argument, and never print, log or repeat
  one. `sshpass -p` and `ssh -o PasswordAuthentication` are not a fallback.
- After `connect`, unset the variable and delete the server-info file.
- After the first password login assh installs `~/.ssh/id_agent_ed25519` on
  the host; later commands authenticate with that key.

## Destructive commands

`session exec`, `session exec-async`, `exec` and `fleet exec` run a classifier
before sending the command. It blocks `rm -r`, `rm` on system paths,
`find -delete`, `mkfs`/`wipefs`/`shred`, `dd` to devices or absolute paths,
recursive `chmod`/`chown` on system paths, and redirects into system paths.
`transfer rm` refuses system paths.

On `dangerous_command_requires_confirmation`, ask the user. If they agree,
rerun through `session exec --confirm-danger`. No other command has that flag.

The classifier matches patterns. It does not see inside scripts passed with
`--stdin`, interpreters (`python -c`), or `xargs`, and it does not classify
`docker rm`, `DROP DATABASE`, `git reset --hard`, `systemctl stop`, `reboot`.
Treat it as a seat belt and judge destructive commands yourself.

Operators can add deny rules in `~/.config/assh/safety.rules` (one command
name per line, mode 0600). An invalid file fails closed with
`safety_policy_invalid`.

## Redaction

Stored and served output is masked for AWS keys, JWTs, bearer tokens, PEM
private keys, provider tokens (`ghp_`, `xoxb-`, `sk-`), URL passwords and
`*password*`/`*secret*`/`*token*`/`*api_key*` assignments. A masked value shows as
`[REDACTED:type]` and the response has `"redacted":true`.

That response is a success: do not rerun the command to get the value.
`--no-redact` (on `exec`, `session read`, `session job-status`,
`transfer read`) is for the case where the user needs the raw value.

Redaction is pattern-based and misses unusual formats. The remote files are
not masked, only what assh stores and returns.

## Host keys

`--host-key-policy accept-new` (default) trusts a host on first contact and
refuses a changed key. `strict` requires the key to be known already.
`no-check` disables verification and is for disposable lab hosts only.

## Audit

`assh audit --last 20` lists recent actions. Entries hold a hash of the
command, not its text. The session transcript on the local machine does hold
command text, so do not inline secrets in commands.
