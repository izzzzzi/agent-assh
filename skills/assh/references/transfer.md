# assh Transfer — File Operations

All file operations work over ssh (scp or ssh pipe, no SFTP dependency).

## List files

```bash
assh transfer list -H HOST -u USER --path /var/log
```

## Stat a file

```bash
assh transfer stat -H HOST -u USER --path /etc/nginx.conf
```

## Upload

```bash
assh transfer put -H HOST -u USER ./local-file /remote/path
```

## Download

```bash
assh transfer get -H HOST -u USER /remote/file ./local-path
```

## Sync (push/pull)

```bash
assh transfer sync --direction push --source ./dist --dest /var/www -H HOST -u USER
assh transfer sync --direction pull --source /var/log --dest ./logs -H HOST -u USER
```

## Read remote file (over ssh, returns output_id)

```bash
assh transfer read -H HOST -u USER --path /etc/app.conf
# {"ok":true,"output_id":"ABC123","stdout_lines":42,"redacted":true,...}
assh read --id ABC123 --limit 50
```

## Directories

```bash
assh transfer mkdir -H HOST -u USER --path /opt/newapp
assh transfer rm -H HOST -u USER --path /tmp/junk
assh transfer rm -H HOST -u USER --path /tmp/old --recursive
```

## Move/rename

```bash
assh transfer mv -H HOST -u USER --source /tmp/a --dest /tmp/b
```

## transfer read errors

| Error | Meaning | Hint |
|-------|---------|------|
| `remote_file_not_found` | File doesn't exist | `assh transfer stat ...` |
| `not_a_file` | Path is a directory | `assh transfer list ...` |
| `file_too_large` | Exceeds byte limit | Use `transfer get` |
| `binary_file` | NUL bytes detected | Use `transfer get` |
| `permission_denied` | Can't read file | `assh transfer stat ...` |
