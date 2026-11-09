---
name: assh
description: "Use when SSH, remote commands, logs, file transfer, deploy, or server inspection is needed — bootstraps connections, manages tmux sessions, keeps output out of agent context."
homepage: https://github.com/izzzzzi/agent-assh
license: MIT
---

# assh — SSH for LLM agents

For any SSH work use `assh`, not raw `ssh`/`scp`/`rsync`: command output stays
on the server and you read only the lines you need. If `assh` is missing, run
`npm i -g agent-assh`.

## Connect

| You have | Command |
| --- | --- |
| Key file | `assh connect -H HOST -u USER -i KEY -n NAME` |
| `~/.ssh/config` alias | `assh connect --ssh-config ALIAS -n NAME` |
| Password | `assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME`, then `unset PASSWORD_ENV` |
| Pasted provider block | write it to a `0600` file, `assh connect-info --file FILE -n NAME`, delete the file |

Optional on `connect`: `-p PORT`, `-J JUMP`, `--reuse` (return a live session
for this host instead of opening another), `--force-pty` (gateways that reject
`-T`, such as RunPod), `--profile readonly|ops` (allow-list for the session).
`connect-info` takes only `-p`, `-J` and `-i`.

`connect` returns `sid`, `identity` and `next_commands`. Use the `sid` below.

## Run a command, then read its output

```bash
assh session exec -s SID -- "cmd"
# {"ok":true,"rc":0,"seq":3,"stdout_lines":120,"stderr_lines":0,...}
assh session read -s SID --seq 3                        # JSON: content, total_lines, has_more
assh session read -s SID --seq 3 --offset 50 --limit 50 # next page (default limit is 50)
assh session read -s SID --seq 3 --stream stderr --raw  # plain text, no JSON wrapper
```

- Pass the command as one quoted string. `cd` and `export` persist between execs.
- `ok:true` means assh ran the command; `rc` is its exit status.
- Read the `seq` that exec returned. Numbers are consumed by failed execs too, so gaps are normal.
- Run one exec at a time per `sid`: they share one shell and run in order.
- exec waits 300 s by default (`-t SECONDS`). After `timeout` the command is
  **still running** and later execs queue behind it: read its `seq` later, do
  not rerun it.
- Pages are counted in lines, not bytes, so one huge line comes back whole.
  Trim on the server: `| cut -c1-500`, `| tail -n 100`.
- stdin: `cat script.sh | assh session exec -s SID --stdin -- "bash -s"`
  (4 MiB max). Without `--stdin` piped input is rejected.

## Long-running commands

```bash
assh session exec-async -s SID -- "make build"     # returns job_id
assh session job-status -s SID --job-id JOB -n 50  # status, rc, last 50 lines in output
assh session job-cancel -s SID --job-id JOB
```

`status` is `running`, `completed` (with `rc`), `cancelled` or `not_found`.
Use this for anything that may exceed the exec wait or has no known duration.

When the work is done, close the session: `assh session close -s SID`.

## Files and one-off commands

These take `-H HOST -u USER` (plus `-i KEY`, `-p`, `-J`), not a `sid`. Pass the
`identity` that `connect` returned as `-i`.

| Task | Command |
| --- | --- |
| Read a text file | `assh transfer read -H HOST -u USER --path FILE`, then `assh read --id OUTPUT_ID` |
| Upload / download | `assh transfer put -H HOST -u USER LOCAL REMOTE` / `assh transfer get -H HOST -u USER REMOTE LOCAL` |
| List / stat | `assh transfer list -H HOST -u USER --path DIR` / `assh transfer stat ... --path FILE` |
| Sync a directory | `assh transfer sync --direction push --source SRC --dest DST -H HOST -u USER` (`pull` for the other way) |
| One command, no session | `assh exec -H HOST -u USER -- "cmd"`, then `assh read --id OUTPUT_ID` |

## Errors

| `error` | What to do |
| --- | --- |
| `dangerous_command_requires_confirmation` | Ask the user. Only `session exec` accepts `--confirm-danger`; `exec`, `exec-async`, `fleet` and `transfer rm` refuse outright. |
| `command_not_allowed` | The session profile forbids it. Report it; do not work around it. |
| `session_unreachable`, `session_stale`, `session_not_found` | Stop using that `sid` and reconnect with the same auth. |
| `output_not_found` | That `seq` produced no output. `assh session list` shows the current `seq`. |
| `timeout` | See above: the command is still running. |
| `invalid_args` | Run `assh <command> --help`. Do not guess flags. |

`[REDACTED:type]` with `"redacted":true` means a secret was masked on purpose
and the command succeeded. Do not retry to recover the value; `--no-redact`
exists on `exec`, `session read`, `session job-status` and `transfer read`.

## Limits

- The safety classifier and profiles catch accidents; they are not a security
  boundary. `docker rm`, `DROP TABLE`, `git reset --hard` and the like are not
  classified, so think before running them.
- Redaction is best-effort: do not print files you know hold secrets.
- Never put a password in a command argument and never echo one.

## References

Load only the one you need:

- [connect](references/connect.md) — auth methods, profiles, connect errors
- [session](references/session.md) — exec options, jobs, processes, cleanup
- [transfer](references/transfer.md) — file operations and their errors
- [server](references/server.md) — services, scan, fleet, port forwarding
- [security](references/security.md) — passwords, safety rules, redaction, host keys
