Use `assh` for SSH work so large remote output stays out of the agent context.

Install: `npm i -g agent-assh && assh version`

Connect:
- With key: `assh connect -H HOST -u USER -i KEY -n NAME`
- With password: `assh connect -H HOST -u USER -E PASS_ENV -n NAME`
- Provider block: save to 0600 temp → `assh connect-info --file TMP -n NAME`
- Picky gateway: add `--force-pty`

Session: `assh session exec -s SID -- "cmd"` then `assh session read -s SID --seq N --limit 50`
Files: `assh transfer put/get/read/list/stat/mkdir/rm/mv/sync`
Services: `assh session service -s SID --action restart --service nginx`
Docker: `assh session docker-ps/docker-logs/docker-exec -s SID`
DB (read-only): `assh session db-query -s SID --type mysql -d DB -q "SELECT"`
Multi-host: `assh fleet exec -H H1 -H H2 -u root -- "cmd"`
Scan: `assh scan -H HOST -u USER`

Security: passwords only through env vars, no `--password` flag. `[REDACTED:type]` is intentional — do not retry. `dangerous_command_requires_confirmation` → ask user before `--confirm-danger`. `transfer read` reads remote files over ssh. Never put passwords in arguments.
