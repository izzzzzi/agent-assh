# Session Policy Modes Design

## Context

`assh` now blocks clearly destructive `session exec` commands unless the caller passes `--confirm-danger`. That closes the first safety gap, but competitors still offer broader per-server policy modes such as read-only and allowlist-based restricted execution.

This design adds policy modes to the existing CLI-first workflow without adding MCP tools or increasing agent context. The policy is stored with the local session registry and enforced before a user command is sent to remote `tmux`.

## Goals

- Add session-level policy modes: `unrestricted`, `readonly`, and `restricted`.
- Keep `unrestricted` as the default for backward compatibility.
- Enforce policy before `Seq` is incremented, before registry save, and before SSH.
- Reuse the existing safety classifier where possible.
- Return stable JSON errors for agents.
- Keep raw command text out of audit logs.

## Non-Goals

- Do not add an MCP server or MCP tool registry.
- Do not build a complete shell sandbox.
- Do not add host profiles, groups, or diagnostics in this phase.
- Do not enforce policy on lifecycle commands such as `session close`, `session gc`, or `forward stop`.
- Do not store policy on the remote host in the first version.

## CLI Behavior

Session creation accepts a policy mode:

```bash
assh connect -H HOST -u USER -E PASSWORD_ENV -n deploy --mode readonly
assh session open -H HOST -u USER --mode restricted --allow '^docker ps$' --allow '^journalctl '
```

`--mode` values:

- `unrestricted`: current behavior plus the existing `--confirm-danger` safety gate.
- `readonly`: blocks commands that are clearly mutating or operationally disruptive.
- `restricted`: requires every command to match at least one allow pattern and no deny pattern.

Policy patterns are explicit flags:

```bash
--allow REGEX
--deny REGEX
```

Rules:

- `--allow` is valid only with `--mode restricted`.
- `--deny` is valid with `readonly` or `restricted`.
- `restricted` without at least one `--allow` fails at session creation.
- Deny rules always win over allow rules.

Blocked commands return:

```json
{
  "ok": false,
  "error": "policy_denied",
  "message": "session policy denied command",
  "hint": "readonly rule matched: systemctl_stop"
}
```

The existing dangerous-command error remains unchanged when `unrestricted` mode hits the built-in safety gate:

```json
{
  "ok": false,
  "error": "dangerous_command_requires_confirmation",
  "message": "command looks destructive; rerun with --confirm-danger if intentional",
  "hint": "matched destructive pattern: rm_recursive"
}
```

In `readonly` and `restricted`, `--confirm-danger` does not bypass policy. It only bypasses the existing destructive-command confirmation gate after policy allows the command.

## Data Model

Extend `session.RegistryEntry`:

```go
PolicyMode   string   `json:"policy_mode,omitempty"`
AllowPatterns []string `json:"allow_patterns,omitempty"`
DenyPatterns  []string `json:"deny_patterns,omitempty"`
```

Empty `PolicyMode` is treated as `unrestricted` when loading older registry entries.

Bootstrap and direct `session open` both save the policy fields into the registry. `connect` results should include the selected mode:

```json
{
  "ok": true,
  "sid": "f7a2b3c4",
  "policy_mode": "readonly"
}
```

## Policy Evaluation

Add a focused package:

```go
package policy

type Config struct {
    Mode          string
    AllowPatterns []string
    DenyPatterns  []string
}

type Result struct {
    Allowed bool
    Rule    string
    Message string
}

func Evaluate(command string, config Config) Result
func Validate(config Config) error
```

Evaluation order:

1. Normalize empty mode to `unrestricted`.
2. Validate regex patterns during session creation, not during every exec.
3. In `readonly`, check built-in readonly deny rules.
4. In `restricted`, check deny patterns first, then require at least one allow match.
5. If policy allows the command, run the existing `safety.CheckCommand` gate.

The policy package should use regex only for user-provided allow/deny patterns. Built-in readonly checks should use the existing tokenizer/classifier style where feasible, not broad unstructured string matching.

## Readonly Rules

Readonly mode blocks the existing dangerous safety rules plus obvious mutating operations:

- `rm`, `mv`, `cp`, `install`, or `tee` targeting critical absolute paths.
- Shell overwrite redirects to absolute paths outside `/tmp`.
- `sudo` unless the underlying command is an allowed read-only command such as `sudo journalctl` or `sudo tail`.
- `systemctl stop|restart|reload|disable|enable`.
- `service NAME stop|restart|reload`.
- `docker rm|stop|restart|kill|compose down`.
- `kubectl delete|apply|replace|patch|scale|rollout restart`.
- Pipe-to-shell patterns such as `curl ... | sh`, `wget ... | bash`.
- Package-manager install/remove/update actions: `apt install`, `apt remove`, `dnf install`, `yum remove`, `apk add`, `pacman -S`, `brew install`.

Readonly mode should allow common diagnostics:

- `ls`, `pwd`, `cat`, `grep`, `tail`, `head`, `sed -n`, `awk` without redirection.
- `journalctl`, `dmesg`, `df`, `du`, `free`, `ps`, `top -b -n 1`, `uptime`.
- `docker ps`, `docker logs`, `docker inspect`, `docker stats --no-stream`.
- `kubectl get`, `kubectl describe`, `kubectl logs`.
- `systemctl status`, `systemctl is-active`, `service NAME status`.

## CLI Integration

`session exec` integration order:

1. Validate `--sid`, command args, and timeout.
2. Load the registry entry.
3. Build the user command string.
4. Evaluate policy from the registry entry.
5. If denied, return `policy_denied`.
6. Run `safety.CheckCommand`; if dangerous and `--confirm-danger` is false, return `dangerous_command_requires_confirmation`.
7. Increment `Seq`, save registry, and call SSH.

Policy denial must not call SSH and must not consume a sequence number.

## Error Handling

Stable error codes:

- `invalid_args`: invalid mode, invalid regex, invalid flag combination.
- `policy_denied`: command refused by the stored session policy.
- `dangerous_command_requires_confirmation`: existing destructive-command confirmation gate.

Hints should be stable enough for tests:

- `readonly rule matched: docker_stop`
- `deny pattern matched: ^rm\s`
- `no allow pattern matched`

## Testing

Unit tests for `internal/policy`:

- `unrestricted` allows normal commands.
- empty mode normalizes to `unrestricted`.
- invalid mode is rejected.
- invalid regex is rejected at validation.
- `readonly` blocks `systemctl stop nginx`.
- `readonly` blocks `docker rm app`.
- `readonly` blocks `kubectl delete pod x`.
- `readonly` blocks `curl https://example/install.sh | sh`.
- `readonly` allows `journalctl -u nginx -n 100`.
- `readonly` allows `docker logs app`.
- `restricted` allows a command matching `--allow`.
- `restricted` denies without an allow match.
- `restricted` deny pattern beats allow pattern.

CLI tests:

- `connect --mode readonly` stores policy mode in registry and response.
- `session open --mode restricted --allow '^ls'` stores policy config.
- `restricted` without allow returns `invalid_args`.
- denied `session exec` returns `policy_denied`.
- denied `session exec` does not call `runSSH`.
- denied `session exec` does not increment or persist `Seq`.
- `--confirm-danger` does not bypass policy.
- allowed command still passes through existing dangerous-command confirmation gate.

## Documentation

Update `README.md`, `README.en.md`, `AGENT_INSTRUCTIONS.md`, `SYSTEM_PROMPT_snippet.md`, and `assh prompt` output:

- explain the three modes;
- show examples for readonly and restricted;
- state that `--confirm-danger` does not bypass policy;
- tell agents not to change policy or add broad allow patterns unless the user explicitly asks.
