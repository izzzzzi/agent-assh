# assh v1.0 Release Design

Date: 2026-05-14
Status: Awaiting user review

## Context

`assh` is a Go CLI for SSH workflows used by LLM agents. Its core value is token economy and persistent sessions:

- `exec` stores large stdout/stderr locally and returns metadata.
- `read` pages selected output only when the agent needs it.
- `session` keeps remote cwd/env alive through `tmux`.

The current implementation already has the main Go structure and tests. The old Bash MVP was useful during development, but v1 ships the Go CLI as the only supported implementation. The gap for v1.0 is that documentation, CLI help, release infrastructure, npm installation, and some advertised workflow features are not yet aligned.

v1.0 should be a stable first public release, not a minimal internal milestone.

## Goals

- Ship a complete v1.0 workflow for agents: `exec`, `read`, `session`, `capabilities`, `scan`, `key-deploy`, and `audit`.
- Make the README contract match real CLI behavior.
- Add missing workflow features already promised by docs where they are small enough for v1.0.
- Publish GitHub Release artifacts for common OS/architecture targets.
- Publish npm package `agent-assh`, which installs command `assh`.
- Add practical CI, linting, release checks, and package smoke tests.
- Provide English and Russian documentation.

## Non-Goals

- Do not add a native SSH backend in v1.0.
- Do not add config files or retention policy management in v1.0.
- Do not support persistent sessions for Windows remote hosts in v1.0.
- Do not silently install packages on remote hosts.
- Do not add GUI, web UI, or a daemon.
- Do not add aggressive style-only lint rules that slow the first stable release.

## Architecture

Keep the current Go layout and strengthen existing boundaries:

- `cmd/assh`: binary entrypoint.
- `internal/cli`: Cobra commands, argument validation, JSON response boundary.
- `internal/transport`: system OpenSSH subprocess backend.
- `internal/state`: local stdout/stderr storage and pagination.
- `internal/session`: remote tmux command generation, local registry, lifecycle.
- `internal/capabilities`: remote probe command and parser.
- `internal/audit`: JSONL events and filtering.

The main behavior change is a clear remote exit-code policy:

- Plain `assh exec`: remote non-zero exit is a command result, not an `assh` failure. Return `ok:true`, `exit_code`, `output_id`, and line counts.
- `assh session exec`: remote command `rc != 0` is also a command result. Return `ok:true`, `rc`, `seq`, and line counts. The workflow succeeded even if the remote command failed.
- Lifecycle commands such as `session open`, `session close`, `session gc`, `capabilities`, `scan`, and `key-deploy`: remote command failure is an `assh` workflow failure and returns a structured JSON error.

## CLI Contract

All operational commands return JSON by default unless an explicit raw mode is requested.

### `assh exec`

Runs one remote command and stores stdout/stderr locally.

Response fields:

- `ok`
- `exit_code`
- `output_id`
- `stdout_lines`
- `stderr_lines`

Remote command failure is represented by `exit_code`, not by a failed CLI operation.

### `assh read`

Reads stored output by stream and line range.

Flags:

- `--id`
- `--stream stdout|stderr`
- `--offset`
- `--limit`
- `--raw`

JSON response fields:

- `ok`
- `output_id`
- `stream`
- `offset`
- `limit`
- `total_lines`
- `has_more`
- `content`

With `--raw`, print only `content` so agents and users can pipe it into other commands.

### `assh session`

Persistent workflow uses remote `tmux` and a local registry.

`session open`:

- Validates host, port, timeout, TTL, and host key policy.
- Verifies `tmux`.
- If `tmux` is missing and `--install-tmux` is not set, returns `tmux_missing`.
- With `--install-tmux`, attempts only explicit non-interactive installation.
- Writes trusted remote metadata and starts tmux before saving local registry.
- Returns `sid`, `session`, `host`, `user`, and `tmux_name`.

`session exec`:

- Accepts `--sid` and `--timeout`.
- Runs command inside the trusted tmux session.
- Stores remote stdout/stderr under the session directory.
- Returns `ok:true`, `rc`, `seq`, `stdout_lines`, `stderr_lines`, `sid`, and `session`.

`session read`:

- Accepts `--sid`, `--seq`, `--stream`, `--offset`, `--limit`, and `--raw`.
- Matches `assh read` behavior for JSON and raw output.

`session close`:

- Closes only a session found in local registry.
- Kills only the matching `tmux_name`.
- Removes only a valid `~/.assh/sessions/<sid>` directory after metadata validation.

`session gc`:

- Defaults to dry-run.
- Supports `--older-than`, `--host`, and `--execute`.
- Reports candidates in JSON.
- With `--execute`, removes local and remote sessions only when metadata proves they were created by `assh`.

### `assh capabilities`

Returns remote environment information:

- OS family.
- `tmux` availability.
- Package manager.
- Whether non-interactive install appears possible.
- Session backend status.

### `assh scan`

Returns a JSON inventory object for the host. v1.0 should document only fields actually returned by the implementation.

### `assh key-deploy`

Deploys the configured public SSH key using a password read from an environment variable.

Rules:

- Password is never accepted as a command-line value.
- Password is used only through askpass-style flow.
- Response is JSON.

### `assh audit`

Reads local JSONL audit events and returns JSON.

Flags:

- `--last`
- `--failed`
- `--host`

Audit entries must not include raw command text or secrets. Command text can be represented by a hash.

## Error Handling

Stable error codes for v1.0:

- `invalid_args`
- `auth_failed`
- `host_key_failed`
- `connection_error`
- `timeout`
- `command_failed`
- `ssh_missing`
- `tmux_missing`
- `tmux_install_failed`
- `unsupported_remote_session_backend`
- `session_not_found`
- `output_not_found`
- `internal_error`

Each JSON error response includes:

- `ok:false`
- `error`
- optional `message`
- optional `hint`

## Documentation

Documentation is release-facing.

Files:

- `README.md`: primary English README for GitHub and npm.
- `README.ru.md`: full Russian README.
- `AGENT_INSTRUCTIONS.md`: short practical guide for LLM agents.
- `SYSTEM_PROMPT_snippet.md`: compact prompt snippet.

README content:

- Product summary.
- Installation through `npm i -g agent-assh`.
- Installation from GitHub Releases.
- Quick start.
- Complete workflow examples.
- JSON response contract.
- Security notes.
- Supported platforms.
- Release and checksum notes.

Badges:

- CI status.
- Go version.
- GitHub Release.
- npm version for `agent-assh`.
- License.
- Go Report Card when a public GitHub repo is available.

Documentation must not mention unsupported flags or fields. v1.0 implements `--raw`, audit filters, and remote `session gc`, so the README files and agent instructions document them. Fields not implemented in v1.0, such as `cwd`, do not appear in examples.

## Release and Distribution

GitHub Releases are the source of binary artifacts.

Use GoReleaser to build:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`

Artifacts:

- OS/architecture archives.
- `checksums.txt`.
- changelog/release notes.

Tags use semantic version format, starting with `v1.0.0`.

Add `assh version` with:

- `version`
- `commit`
- `date`
- `go_version`

GoReleaser passes version data through ldflags.

## npm Package

The npm package name is `agent-assh`.

Behavior:

- `npm i -g agent-assh` installs command `assh`.
- The package is a thin installer and wrapper around GitHub Release binaries.
- Install script detects `process.platform` and `process.arch`.
- It downloads the matching GitHub Release archive.
- It verifies checksum.
- It stores the binary inside the package directory.
- JS wrapper forwards arguments and exit code to the downloaded binary.

Package files:

- `package.json`
- npm install script
- JS wrapper under a package-owned `bin` or `scripts` path
- `.npmignore`
- smoke tests for platform mapping and wrapper execution

The npm package should not compile Go during normal install.

## CI and Quality Gates

GitHub Actions jobs:

- `test`: `gofmt` check, `go vet ./...`, `go test ./...`.
- `race`: `go test -race ./...` on Linux.
- `lint`: `golangci-lint` and markdown lint.
- `release-check`: `goreleaser check`.
- `npm-smoke`: npm pack/install/wrapper smoke checks without publishing.
- `release`: on tags matching `v*`, publishes GitHub Release artifacts and npm package.

Go lint profile:

- `errcheck`
- `govet`
- `ineffassign`
- `staticcheck`
- `unused`
- `misspell`
- basic `gocritic` if it is not noisy

Do not enable strict complexity, mandatory comments, or broad style-only rules for v1.0.

Markdown/docs:

- Use `markdownlint-cli2`.
- Link checking is not a blocking v1.0 gate because badges and release URLs may not exist before the first public release.

## Testing Strategy

Add focused tests for:

- CLI argument validation and JSON errors.
- `read --raw`.
- `session read --raw`.
- lifecycle failure mapping, including `tmux_missing`.
- `session open` not saving local registry when remote setup fails.
- `session gc` dry-run and execute candidate selection.
- audit filters.
- `assh version` output.
- npm platform mapping and wrapper behavior.

Keep SSH-dependent tests mocked or fake-binary based unless a real integration environment is explicitly configured.

## Acceptance Criteria

- `go test ./...` passes.
- `go vet ./...` passes.
- `gofmt` check passes.
- `go test -race ./...` passes in CI.
- `golangci-lint` passes with the v1.0 profile.
- Markdown lint passes for release docs.
- `goreleaser check` passes.
- GoReleaser can produce local snapshot artifacts.
- npm package smoke test verifies command `assh` dispatch.
- README examples match real command behavior.
- `npm i -g agent-assh` installs `assh` by downloading GitHub Release binaries.
- Tag `v1.0.0` can publish GitHub Release artifacts and npm package.
