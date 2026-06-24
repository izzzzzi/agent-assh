# assh Server — Management Commands

## Service Management

```bash
assh session service -s SID --action status --service nginx
assh session service -s SID --action restart --service docker
assh session service -s SID --action start --service postgresql
assh session service -s SID --action stop --service apache2
assh session service -s SID --action logs --service nginx --lines 100
```

## Docker

```bash
assh session docker-ps -s SID
assh session docker-ps -s SID -a               # all containers
assh session docker-logs -s SID --container myapp --tail 100
assh session docker-exec -s SID --container myapp -- "ls -la /app"
```

## Database Query (Read-Only)

Only SELECT, SHOW, DESCRIBE, EXPLAIN queries are allowed:

```bash
assh session db-query -s SID --type mysql -d mydb -q "SELECT COUNT(*) FROM users"
assh session db-query -s SID --type postgres -d mydb -q "SELECT * FROM orders LIMIT 10"
assh session db-query -s SID --type mysql -d mydb -U dbuser -W dbpass -q "SHOW TABLES"
```

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
