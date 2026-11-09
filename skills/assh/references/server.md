# assh server operations

## Services (systemd)

```bash
assh session service -s SID --action status --service nginx
assh session service -s SID --action restart --service nginx
assh session service -s SID --action logs --service nginx --lines 100
```

Actions: `status`, `start`, `stop`, `restart`, `logs`. `start`, `stop` and
`restart` run through `sudo`. The response carries the command output in
`output`, redacted.

## Docker, databases, everything else

No dedicated subcommands. Run the tool through the session:

```bash
assh session exec -s SID -- "docker ps -a"
assh session exec -s SID -- "docker logs myapp --tail 100"
assh session exec -s SID -- "psql mydb -c 'SELECT count(*) FROM users'"
```

## Host inventory

```bash
assh scan -H HOST -u USER
# {"hostname":"web01","os":"Linux","kernel":"6.1.0","arch":"x86_64","cpu_cores":"4",
#  "ip":"10.0.0.1","uptime":"30 days","load":"0.15 0.10 0.05",
#  "mem_total_kb":"8176248","mem_avail_kb":"5241360","disk":"12G/50G (25%)"}
```

## Several hosts at once

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

## Port forwarding

```bash
assh forward start --name db -H HOST -u USER --local-forward 5433:127.0.0.1:5432
assh forward status --name db
assh forward stop --name db
```

Also `--remote-forward` and `--dynamic-forward`. The tunnel stays up for
`--persist` (default 1h). `start` fails if the port cannot be bound.

## Let a human watch

```bash
assh session watch -s SID   # returns attach_cmd for the user's own terminal
```
