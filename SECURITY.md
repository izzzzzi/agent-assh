# Security Policy

## Supported versions

Security fixes go into the latest release. Update with `npm i -g agent-assh@latest`.

## Reporting a vulnerability

Report privately through GitHub: open the repository's **Security** tab and
choose **Report a vulnerability**
(<https://github.com/izzzzzi/agent-assh/security/advisories/new>). Do not open a
public issue for a suspected vulnerability.

Include the assh version (`assh version`), the command you ran, and what
happened. Leave real hostnames, keys and passwords out of the report.

## Scope

`assh` runs the system `ssh`, `scp` and `rsync` on your behalf and stores
session state and command output on the local machine. Two parts are guardrails
rather than security boundaries, and bypasses of them are handled as ordinary
bugs:

- the destructive-command classifier and session profiles;
- output redaction, which is pattern-based.

The remote account's own privileges are the boundary to rely on.
