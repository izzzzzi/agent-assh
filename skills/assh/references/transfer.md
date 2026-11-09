# assh transfer

Every command takes `-H HOST -u USER` and optionally `-i KEY`, `-p PORT`,
`-J JUMP`. Without `-u` the user is `root`. Pass the `identity` from the
`connect` response as `-i`. A leading `~/` in a remote path expands to the
remote home.

## Read a text file

```bash
assh transfer read -H HOST -u USER --path /etc/app.conf
# {"ok":true,"output_id":"ID","stdout_lines":42,"redacted":true,...}
assh read --id ID --offset 0 --limit 50
```

Refuses directories, binary files and files over `--max-bytes` (default
1 MiB). Download those with `transfer get`.

## Upload and download

```bash
assh transfer put -H HOST -u USER ./local-file /remote/path
assh transfer get -H HOST -u USER /remote/file ./local-path
```

Single files, via scp. `put` creates missing remote parent directories. Both
overwrite the destination without asking.

## Sync a directory

```bash
assh transfer sync --direction push --source ./dist --dest /var/www -H HOST -u USER
assh transfer sync --direction pull --source /var/log/app --dest ./logs -H HOST -u USER
```

Needs `rsync` on both machines. `--exclude PATTERN` is repeatable. `--delete`
removes files on the destination that the source lacks: confirm with the user
first. The remote path must not contain shell metacharacters.

## List, stat, mkdir, mv, rm

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

## Errors

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
