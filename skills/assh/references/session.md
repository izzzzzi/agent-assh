# assh session

## exec

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

## read

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

## Timeouts

A `timeout` error means assh stopped waiting. The command keeps running in the
shell and blocks the commands after it. Read its `seq` later, or cancel it
yourself (`assh session ps`, `assh session kill`). For work longer than the
wait, start it as a job.

## Jobs

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

## Processes

```bash
assh session ps -s SID --top 20
assh session ps -s SID --filter nginx
assh session kill -s SID --pid 1234 --signal KILL
```

## Lifecycle

```bash
assh session list                           # sid, host, seq, expired
assh session close -s SID                   # kills tmux, removes remote files
assh session gc --older-than 24h --execute  # without --execute it only lists
```

`expired:false` in `session list` reflects the TTL only. A session can still be
dead on the server; then exec returns `session_unreachable` or `session_stale`
and you reconnect.

## Errors

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
