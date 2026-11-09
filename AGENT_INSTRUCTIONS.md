<!-- Generated from skills/assh/ by scripts/check-skill-copies.js. Edit the skill, not this file. -->

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

## assh connect

### Methods

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

#### Pasted provider block

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

### Options

| Flag | Use |
| --- | --- |
| `-p PORT`, `-J JUMP` | non-default port, jump host |
| `--reuse` | return a live session for the same host, user, key and profile instead of opening a new one |
| `--force-pty` | gateways that reject `-T` (RunPod); stderr is merged into stdout in this mode |
| `--profile NAME` | restrict the session, see below |
| `--no-install-tmux` | fail with `tmux_missing` instead of installing tmux |
| `--ttl 12h` | session lifetime in the local registry |
| `--host-key-policy` | `accept-new` (default), `strict`, `no-check` |

### Response

`ok`, `sid`, `session` (your `-n` label), `tmux_name`, `host`, `user`,
`identity` (the key to pass as `-i` to `transfer` and `exec`), `key_deployed`,
`reused` (only with `--reuse`), `next_commands` (`exec`, `read`, `list`,
`close` with the sid filled in).

### Profiles

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

### Errors

| Error | Meaning | Fix |
| --- | --- | --- |
| `auth_failed` | key or password rejected | check credentials; with a password use `-E` |
| `tmux_missing` | tmux is not installed or not on the non-interactive `PATH` | drop `--no-install-tmux`, or install tmux on the host |
| `host_key_failed` | the host key changed since it was first seen | stop and tell the user; do not switch to `no-check` on your own |
| `connection_error`, `timeout` | network problem | check host, port, reachability |

## assh session

### exec

```bash
assh session exec -s SID -- "pwd"
# {"ok":true,"rc":0,"seq":1,"stdout_lines":1,"stderr_lines":0,"sid":"SID","session":"deploy"}
```

| Flag | Use |
| --- | --- |
| `-t SECONDS` | how long to wait for the command (default 300) |
| `--stdin` | forward piped stdin (4 MiB max) |
| `--before CMD`, `--after CMD` | run around the main command in the same shell |
| `--confirm-danger` | run a command the safety classifier blocked, after the user agreed |

The command runs in the session's tmux shell, so `cd`, `export` and shell
functions persist. Commands run strictly one after another.

### read

```bash
assh session read -s SID --seq 1                         # stdout, lines 1-50
assh session read -s SID --seq 1 --offset 50 --limit 100 # lines 51-150
assh session read -s SID --seq 1 --stream stderr
assh session read -s SID --seq 1 --raw                   # content only
```

JSON form: `content`, `total_lines`, `has_more`, `offset`, `limit`, `redacted`,
`redaction_count`. Use JSON when you need `has_more`; use `--raw` when you just
want the text.

`seq` counts attempts. It is allocated before the command runs, so a timed-out
or failed exec still owns its number and reading a number that produced nothing
returns `output_not_found`.

### Timeouts

A `timeout` error means assh stopped waiting. The command keeps running in the
shell and blocks the commands after it. Read its `seq` later, or cancel it
yourself (`assh session ps`, `assh session kill`). For work longer than the
wait, start it as a job.

### Jobs

```bash
assh session exec-async -s SID -- "make build"
# {"ok":true,"job_id":"JOB","status_cmd":"...","cancel_cmd":"..."}
assh session job-status -s SID --job-id JOB -n 50
# {"ok":true,"status":"completed","rc":0,"output":"...last 50 lines...","redacted":false}
assh session job-status -s SID --job-id JOB --raw
assh session job-cancel -s SID --job-id JOB
```

`status` is `running`, `completed` (then `rc` is present), `cancelled` or
`not_found`. `output` is the tail of the job's combined stdout and stderr; `-n`
sets how many lines. A job runs in its own tmux window under `bash` (`sh` if
bash is absent): it does not share `cd` or variables with `session exec`.
`exec-async` has no `--confirm-danger` and no `--stdin`.

### Processes

```bash
assh session ps -s SID --top 20
assh session ps -s SID --filter nginx
assh session kill -s SID --pid 1234 --signal KILL
```

### Lifecycle

```bash
assh session list                           # sid, host, seq, expired
assh session close -s SID                   # kills tmux, removes remote files
assh session gc --older-than 24h --execute  # without --execute it only lists
```

`expired:false` in `session list` reflects the TTL only. A session can still be
dead on the server; then exec returns `session_unreachable` or `session_stale`
and you reconnect.

### Errors

| Error | Meaning | Fix |
| --- | --- | --- |
| `session_not_found` | unknown or closed sid | reconnect |
| `session_unreachable`, `session_stale` | tmux session or host is gone | reconnect, stop retrying the sid |
| `timeout` | wait window exceeded | command still runs; see Timeouts |
| `command_status_unknown` | exec ended without a result | read the `seq` named in the hint before retrying |
| `stdin_pipe_unsupported` | input was piped without `--stdin` | add `--stdin` or upload with `transfer put` |
| `stdin_too_large` | more than 4 MiB piped | upload with `transfer put` |
| `job_not_found` | wrong `job_id` for this session | use the id `exec-async` returned |
| `dangerous_command_requires_confirmation` | destructive pattern | ask the user, then `--confirm-danger` |
| `command_not_allowed` | blocked by the session profile | report it |

## assh transfer

Every command takes `-H HOST -u USER` and optionally `-i KEY`, `-p PORT`,
`-J JUMP`. Without `-u` the user is `root`. Pass the `identity` from the
`connect` response as `-i`. A leading `~/` in a remote path expands to the
remote home.

### Read a text file

```bash
assh transfer read -H HOST -u USER --path /etc/app.conf
# {"ok":true,"output_id":"ID","stdout_lines":42,"redacted":true,...}
assh read --id ID --offset 0 --limit 50
```

Refuses directories, binary files and files over `--max-bytes` (default
1 MiB). Download those with `transfer get`.

### Upload and download

```bash
assh transfer put -H HOST -u USER ./local-file /remote/path
assh transfer get -H HOST -u USER /remote/file ./local-path
```

Single files, via scp. `put` creates missing remote parent directories. Both
overwrite the destination without asking.

### Sync a directory

```bash
assh transfer sync --direction push --source ./dist --dest /var/www -H HOST -u USER
assh transfer sync --direction pull --source /var/log/app --dest ./logs -H HOST -u USER
```

Needs `rsync` on both machines. `--exclude PATTERN` is repeatable. `--delete`
removes files on the destination that the source lacks: confirm with the user
first. The remote path must not contain shell metacharacters.

### List, stat, mkdir, mv, rm

```bash
assh transfer list -H HOST -u USER --path /var/log
# {"ok":true,"count":5,"entries":[{"name":"syslog","type":"f","size":12345,"mtime":"..."}]}
assh transfer stat -H HOST -u USER --path /etc/nginx.conf
assh transfer mkdir -H HOST -u USER --path /opt/newapp
assh transfer mv -H HOST -u USER --source /tmp/a --dest /tmp/b
assh transfer rm -H HOST -u USER --path /tmp/junk
assh transfer rm -H HOST -u USER --path /tmp/old --recursive
```

`list` returns every entry of the directory inline: for a large directory run
`ls | head` through `session exec` instead. `list` and `stat` need GNU
`find`/`stat` on the host.

### Errors

| Error | Meaning | Fix |
| --- | --- | --- |
| `remote_file_not_found` | path does not exist | check with `transfer list` on the parent |
| `not_a_file` | `read` on a directory | `transfer list` |
| `file_too_large` | over `--max-bytes` | raise `--max-bytes` or `transfer get` |
| `binary_file` | NUL bytes found | `transfer get` |
| `permission_denied` | unreadable for this user | run through `session exec` with sudo |
| `unsupported_remote` | host lacks GNU find/stat (macOS, BSD, BusyBox) | `assh exec -H HOST -u USER -- "ls -la PATH"` |
| `dangerous_command_requires_confirmation` | `rm` on a system path | ask the user, then `session exec --confirm-danger` |
| `sync_failed` | rsync error, text in `message` | fix the cause shown |

## assh server operations

### Services (systemd)

```bash
assh session service -s SID --action status --service nginx
assh session service -s SID --action restart --service nginx
assh session service -s SID --action logs --service nginx --lines 100
```

Actions: `status`, `start`, `stop`, `restart`, `logs`. `start`, `stop` and
`restart` run through `sudo`. The response carries the command output in
`output`, redacted.

### Docker, databases, everything else

No dedicated subcommands. Run the tool through the session:

```bash
assh session exec -s SID -- "docker ps -a"
assh session exec -s SID -- "docker logs myapp --tail 100"
assh session exec -s SID -- "psql mydb -c 'SELECT count(*) FROM users'"
```

### Host inventory

```bash
assh scan -H HOST -u USER
# {"hostname":"web01","os":"Linux","kernel":"6.1.0","arch":"x86_64","cpu_cores":"4",
#  "ip":"10.0.0.1","uptime":"30 days","load":"0.15 0.10 0.05",
#  "mem_total_kb":"8176248","mem_avail_kb":"5241360","disk":"12G/50G (25%)"}
```

### Several hosts at once

```bash
assh fleet exec -H web01 -H web02 -u deploy -i KEY -- "uptime"
# {"ok":true,"hosts":2,"failed":1,"results":[
#   {"host":"web01","ok":true,"exit_code":0,"stdout":"...","stderr":""},
#   {"host":"web02","ok":false,"exit_code":255,"stdout":"","stderr":"...","error":"..."}]}
```

Top-level `ok` only says the fan-out ran: check `failed` and each result.
Output is returned inline, redacted, and cut to the last 8 KiB per stream
(`"truncated":true`), so keep fleet commands short. All hosts use port 22, one
user and one key. Destructive commands are refused with no override. Default
timeout is 60 s per host (`-t`).

### Port forwarding

```bash
assh forward start --name db -H HOST -u USER --local-forward 5433:127.0.0.1:5432
assh forward status --name db
assh forward stop --name db
```

Also `--remote-forward` and `--dynamic-forward`. The tunnel stays up for
`--persist` (default 1h). `start` fails if the port cannot be bound.

### Let a human watch

```bash
assh session watch -s SID   # returns attach_cmd for the user's own terminal
```

## assh security rules

### Passwords

- A password reaches assh only through an environment variable named by `-E`
  (`connect`, `key-deploy`) or inside a server-info file read by
  `connect-info`. There is no `--password` flag.
- Never put a password in a command argument, and never print, log or repeat
  one. `sshpass -p` and `ssh -o PasswordAuthentication` are not a fallback.
- After `connect`, unset the variable and delete the server-info file.
- After the first password login assh installs `~/.ssh/id_agent_ed25519` on
  the host; later commands authenticate with that key.

### Destructive commands

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

### Redaction

Stored and served output is masked for AWS keys, JWTs, bearer tokens, PEM
private keys, provider tokens (`ghp_`, `xoxb-`, `sk-`), URL passwords and
`*password*`/`*secret*`/`*token*`/`*api_key*` assignments. A masked value shows as
`[REDACTED:type]` and the response has `"redacted":true`.

That response is a success: do not rerun the command to get the value.
`--no-redact` (on `exec`, `session read`, `session job-status`,
`transfer read`) is for the case where the user needs the raw value.

Redaction is pattern-based and misses unusual formats. The remote files are
not masked, only what assh stores and returns.

### Host keys

`--host-key-policy accept-new` (default) trusts a host on first contact and
refuses a changed key. `strict` requires the key to be known already.
`no-check` disables verification and is for disposable lab hosts only.

### Audit

`assh audit --last 20` lists recent actions. Entries hold a hash of the
command, not its text. The session transcript on the local machine does hold
command text, so do not inline secrets in commands.
