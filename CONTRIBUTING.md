# Contributing to assh

Thanks for helping improve `assh`. This file is the short version of "how to
land a change without CI yelling at you".

## Setup

You need Go 1.22+ and Node 18+.

```bash
git clone https://github.com/izzzzzi/agent-assh.git
cd agent-assh
npm install            # runs postinstall, which downloads the released binary
npm run hooks:install  # wires .githooks/ as the git hooks path
go build -o native/ ./cmd/assh      # run your working tree through bin/assh.js
```

`postinstall` fetches the binary for the version in `package.json` from GitHub
Releases. On a branch where that version is not released yet, run
`AGENT_ASSH_SKIP_DOWNLOAD=1 npm install`.

After `hooks:install`, every `git commit` runs the pre-commit gate locally
before the commit is created. This mirrors what CI checks, so a green local
commit almost always means a green PR.

## The one command

```bash
npm run check
```

This runs, in order:

1. `gofmt -l .` (must be empty)
2. `go vet ./...`
3. `go test ./...`
4. `npm run smoke` (CLI smoke test)
5. `npm pack --dry-run` (release artifact contract)
6. `node scripts/check-skill-copies.js` (AGENTS.md, AGENT_INSTRUCTIONS.md, SYSTEM_PROMPT_snippet.md, the Cursor/Cline rules and the embedded `assh prompt` are generated from `skills/assh/`; edit the skill, then run the script with `--write`)
7. `markdownlint-cli2` on README, README.en, AGENT_INSTRUCTIONS, SYSTEM_PROMPT_snippet, CONTRIBUTING

For linting only (faster feedback):

```bash
npm run lint        # golangci-lint v2 with the project config
npm run lint:fix    # same, with --fix for auto-fixable findings
```

## Code style rules (enforced by golangci-lint)

These are configured in `.golangci.yml`. The notable limits:

| Rule | Limit | Linter |
| --- | --- | --- |
| Line length | 120 cols | `lll` |
| Function length | 80 lines / 50 statements | `funlen` |
| File length | keep under 500 lines (split by domain) | convention + review |
| Comments that are sentences | end in a period | `godot` |
| Unused predeclared shadowing | rejected | `predeclared`, `gocritic` |
| Style/best-practice | `revive`, `gocritic` | diagnostic+style+performance tags |

Tabs for Go (`gofmt`), spaces for YAML/JSON/Markdown (see `.editorconfig`).

### When a file grows past 500 lines

Split it by domain, not mechanically. The `internal/cli/session*.go` family is
the reference pattern: `session.go` (router + open), `session_exec.go`,
`session_read.go`. Tests follow the same split with a shared
`session_test_helpers_test.go` for cross-file helpers.

### When a function grows past 80 lines

Extract the cobra `RunE` body into a `runXxx` handler that takes the
flag-derived options as a struct or explicit args. See `runSessionExec` /
`sessionExecOptions` in `internal/cli/session_exec.go`.

## Tests

- Every change ships with a test. Tests are table-driven where it makes sense
  (see `internal/safety/safety_test.go` for the canonical style).
- Shared test helpers go in a `*_test_helpers_test.go` file in the same package.
- A test helper that returns a canonical value (e.g. `sessionExecSuccessResult`)
  beats repeating the same literal across tests and keeps lines under the limit.

## Workflow

1. Branch off `main` (direct commits to `main` are blocked by the pre-commit
   hook).
2. Commit with clear messages. Conventional prefixes (`fix:`, `feat:`,
   `docs:`, `chore:`, `test:`) keep the changelog clean.
3. Push and open a PR. CI runs the same `check` plus `-race` and a
   release-config validation.
4. Keep `README.md` (Russian) and `README.en.md` (English) in sync for any
   user-facing change.
5. Agent-facing instructions are edited only in `skills/assh/`; regenerate the
   copies with `node scripts/check-skill-copies.js --write`.

## Releasing

Releases are driven by tags. The `pre-push` hook verifies that a pushed
`vX.Y.Z` tag matches the `package.json` version of the commit it points at;
ordinary pushes are not checked, so `main` may move past the last release.
Tagging triggers `goreleaser` (Go binaries to GitHub Releases) and `npm publish`
(via the `release.yml` workflow). The version lives in `package.json`, the three
`plugin.json` manifests and the entry in `.claude-plugin/marketplace.json`;
`npm run release:contract` fails if they disagree.
