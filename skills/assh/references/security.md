# assh Security — Rules for Agents

## Password Handling (CRITICAL)

- Passwords are accepted ONLY through environment variables
- There is NO `--password` flag — never try it
- `connect-info` reads passwords from stdin or a local file, NEVER from command arguments
- If key login works, `connect` does not read the password env var
- Never print, log, summarize, or repeat passwords in responses

## Output Redaction (Best-Effort Hygiene, NOT a Security Boundary)

Output is redacted by default. Secret patterns (AWS keys, JWTs, bearer tokens,
PEM keys, `password=`/`token=` assignments) are replaced with `[REDACTED:type]`.
When `"redacted":true` appears in a response: **the command succeeded as-is and
the redaction is intentional. Do NOT retry to recover the value.**

Pass `--no-redact` only if you genuinely need the raw output.

## Command Safety

- `session exec` blocks destructive commands (rm -rf, mkfs, wipefs, etc.)
- If you get `dangerous_command_requires_confirmation`, ask the user explicitly
  before re-running with `--confirm-danger`. Never add `--confirm-danger` on your own
- `db-query` is read-only — write operations return a safety error
- Declarative safety policy: `~/.config/assh/safety.rules` can ADD deny rules
  (one command per line, file must be mode 0600)

## Host Key Policy

- Default: `--host-key-policy accept-new`
- For hardened environments: `--host-key-policy strict`
- `--host-key-policy no-check` is unsafe — only for disposable lab/dev hosts

## Command text

Command text is NOT written to audit logs. Audit entries use command hashes.
