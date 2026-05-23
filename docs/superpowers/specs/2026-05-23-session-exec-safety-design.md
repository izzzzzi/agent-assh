# Session Exec Safety Design

## Context

`assh session exec` can send arbitrary shell commands to a remote tmux session. This is powerful, but it also means an agent can accidentally run destructive commands such as recursive deletion or disk-wipe operations. The first safety layer should protect only `assh session exec`; built-in lifecycle commands such as `session close`, `session gc --execute`, and `forward stop` remain unchanged.

## Goals

- Block clearly destructive remote commands before they are sent over SSH.
- Require an explicit opt-in flag when the destructive command is intentional.
- Keep normal noninteractive agent workflows reliable; do not use an interactive prompt.
- Avoid storing raw command text in audit logs.
- Keep the first version simple with built-in rules, not user configuration.

## Non-Goals

- This is not a full shell sandbox or complete bash parser.
- This does not inspect commands run indirectly from scripts or binaries.
- This does not add configurable policy files or per-user allowlists.
- This does not protect `assh` lifecycle commands outside `session exec`.

## CLI Behavior

Safe commands keep the current behavior:

```bash
assh session exec -s SID -- "pwd"
```

Dangerous commands are blocked before any SSH call:

```bash
assh session exec -s SID -- "rm -rf /var/www"
```

The command returns a JSON error:

```json
{
  "ok": false,
  "error": "dangerous_command_requires_confirmation",
  "message": "command looks destructive; rerun with --confirm-danger if intentional",
  "hint": "matched destructive pattern: rm_recursive"
}
```

Intentional destructive commands require an explicit flag:

```bash
assh session exec -s SID --confirm-danger -- "rm -rf /tmp/build"
```

The safety check runs before `Seq` is incremented, before the registry is saved, and before `runSSH` is called. A blocked command therefore must not create remote side effects or consume a session sequence number.

## Dangerous Rules

The first version uses a built-in conservative rule set:

- `rm` with recursive flags: `-r`, `-R`, `-rf`, `-fr`, and combined forms containing recursive deletion.
- `rm` targeting critical paths: `/`, `/etc`, `/var`, `/home`, `/root`, `/usr`, `/bin`, `/sbin`, `/lib`, `/opt`, `/srv`, and root wildcards such as `/*`.
- `find ... -delete`.
- Disk or filesystem wipe tools: `mkfs`, `mkfs.*`, `wipefs`, `shred`.
- `dd` with outputs targeting devices or absolute paths, such as `of=/dev/sda` or `of=/etc/passwd`.
- Shell truncation or overwrite redirection targeting absolute paths, such as `> /etc/passwd` or `: > /var/log/app.log`.
- Recursive permission or ownership changes on critical paths: `chmod -R`, `chown -R`, `chgrp -R`.

The first version intentionally does not block `kill`, `systemctl stop`, `docker rm`, or `kubectl delete`. Those can be destructive, but they also have many normal operational uses and should be considered separately after real usage feedback.

## Command Analysis

Add a focused package:

```go
package safety

type Result struct {
    Dangerous bool
    Rule      string
    Message   string
}

func CheckCommand(command string) Result
```

`CheckCommand` should split simple shell text into command segments around common operators such as `;`, `&&`, `||`, and `|`. It should tokenize enough to avoid matching text inside single or double quotes for command-name rules. For example, `echo "rm -rf /"` should not be blocked.

The tokenizer does not need to execute shell expansion or model every bash grammar edge case. It should be deterministic, well-tested, and conservative for obvious destructive command forms.

## CLI Integration

`newSessionExecCommand` adds:

```bash
--confirm-danger
```

Integration order:

1. Validate `--sid`, command args, timeout, and session registry as today.
2. Build the user command with existing `remoteCommand(args)`.
3. Run `safety.CheckCommand(userCommand)`.
4. If dangerous and `--confirm-danger` is false, return `dangerous_command_requires_confirmation`.
5. Only after passing safety, increment `entry.Seq`, build the remote tmux wrapper, save registry, and call SSH.

Audit behavior remains hash-only. The raw command should not be written to audit logs.

## Error Handling

The blocked-command error should be stable for agents:

- `error`: `dangerous_command_requires_confirmation`
- `message`: fixed short explanation
- `hint`: includes the matched rule id, for example `matched destructive pattern: rm_recursive`

The rule id should be machine-readable and stable enough for tests and agent handling.

## Testing

Unit tests for `internal/safety`:

- blocks `rm -rf /tmp/build`
- blocks `sudo rm -rf /var/www`
- blocks `rm -r /etc`
- blocks `find /tmp -type f -delete`
- blocks `mkfs.ext4 /dev/sdb`
- blocks `wipefs -a /dev/sdb`
- blocks `dd if=/dev/zero of=/dev/sda bs=1M`
- blocks `: > /etc/passwd`
- blocks `chmod -R 777 /etc`
- allows `echo "rm -rf /"`
- allows nonrecursive `rm file.tmp`
- allows safe read-only commands like `ls`, `cat`, `grep`, `tail`

CLI tests:

- dangerous `session exec` without `--confirm-danger` returns `dangerous_command_requires_confirmation`.
- blocked command does not call `runSSH`.
- blocked command does not increment or persist session `Seq`.
- dangerous `session exec` with `--confirm-danger` calls `runSSH` and behaves like existing exec.

## Documentation

Update user-facing docs and prompt snippets with:

- the new `--confirm-danger` flag;
- examples of blocked destructive commands;
- guidance that agents should only add the flag when the user explicitly intends the destructive action.
