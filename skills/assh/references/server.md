# assh Server — Management Commands

## Service Management

```bash
assh session service -s SID --action status --service nginx
assh session service -s SID --action restart --service docker
assh session service -s SID --action start --service postgresql
assh session service -s SID --action stop --service apache2
assh session service -s SID --action logs --service nginx --lines 100
```

## Docker, databases, anything else

There are no dedicated subcommands — run tools directly through the session:

```bash
assh session exec -s SID -- "docker ps -a"
assh session exec -s SID -- "docker logs myapp --tail 100"
assh session exec -s SID -- "docker exec myapp ls -la /app"
assh session exec -s SID -- "mysql -e 'SELECT COUNT(*) FROM users' mydb"
assh session exec -s SID -- "psql mydb -c 'SHOW TABLES'"
```

`safety` guards file-system destructive patterns (rm -rf on system paths, dd
to block devices, redirects into /etc /var ...) — `docker rm`/`DELETE` are not
classified, so think before running them, same as any other shell command.

## Host Scanning

```bash
assh scan -H HOST -u USER
# Returns JSON: hostname, OS, kernel, arch, CPU cores, IP, uptime, load, memory, disk
```

## Session Observability (human watch)

```bash
assh session watch -s SID
# Returns an attach_cmd — paste in a terminal to see agent's tmux in real-time
```
