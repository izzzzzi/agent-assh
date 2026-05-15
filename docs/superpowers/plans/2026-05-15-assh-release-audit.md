# assh Release Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Audit the `assh` release path end to end and apply focused fixes for lint, test, packaging, or tag-driven build defects.

**Architecture:** Treat the current repository as the source of truth and run the same checks that protect a release tag. Keep fixes small and local to the failing boundary: Go code/tests, npm wrapper/install scripts, GoReleaser config, GitHub Actions, package metadata, or release-facing docs.

**Tech Stack:** Go 1.22, Cobra, Node.js/npm, GoReleaser, GitHub Actions, golangci-lint, markdownlint-cli2.

---

## File Structure

- Read `docs/superpowers/specs/2026-05-15-assh-release-audit-design.md`: audit scope and fix policy.
- Read `.github/workflows/ci.yml`: CI parity checks and tool versions.
- Read `.github/workflows/release.yml`: tag trigger, version gate, release/publish order, permissions.
- Read `.goreleaser.yaml`: build matrix, archive naming, checksum naming, ldflags.
- Read `package.json`: package version, `bin`, `files`, npm scripts, package identity.
- Read `bin/assh.js`: installed command wrapper behavior.
- Read `scripts/install.js`: binary download, checksum verification, archive extraction, destination path.
- Read `scripts/platform.js`: Node platform/arch to GoReleaser target mapping.
- Read `scripts/smoke-test.js`: network-free npm wrapper smoke coverage.
- Read `scripts/release-contract-test.js`: release-equivalent snapshot artifact contract check.
- Modify only the files above, Go source/tests under `cmd/` and `internal/`, or release-facing docs when a check exposes a concrete defect.

---

## Task 1: Baseline Inventory

**Files:**
- Read: `docs/superpowers/specs/2026-05-15-assh-release-audit-design.md`
- Read: `.github/workflows/ci.yml`
- Read: `.github/workflows/release.yml`
- Read: `.goreleaser.yaml`
- Read: `package.json`
- Read: `bin/assh.js`
- Read: `scripts/install.js`
- Read: `scripts/platform.js`
- Read: `scripts/smoke-test.js`

- [ ] **Step 1: Confirm clean baseline**

Run:

```bash
git status --short --branch
```

Expected: current branch is `main`. Existing untracked or modified files must be noted before making audit fixes.

- [ ] **Step 2: Record relevant tool availability**

Run:

```bash
go version
node --version
npm --version
command -v golangci-lint || true
command -v goreleaser || true
command -v markdownlint-cli2 || true
```

Expected: Go, Node, and npm are available. Missing optional tools are recorded and covered with the fallback commands in later tasks.

- [ ] **Step 3: Inspect release-critical config**

Run:

```bash
sed -n '1,220p' .github/workflows/ci.yml
sed -n '1,220p' .github/workflows/release.yml
sed -n '1,220p' .goreleaser.yaml
sed -n '1,220p' package.json
```

Expected: CI contains Go tests, race tests, linting, GoReleaser check, npm smoke, and npm pack dry-run. Release runs on `v*` tags, verifies `v$(package.version)` equals `$GITHUB_REF_NAME`, runs GoReleaser, then publishes npm.

- [ ] **Step 4: Inspect npm wrapper and installer contract**

Run:

```bash
sed -n '1,240p' bin/assh.js
sed -n '1,320p' scripts/install.js
sed -n '1,180p' scripts/platform.js
sed -n '1,240p' scripts/smoke-test.js
```

Expected: wrapper executes `native/assh` or `native/assh.exe`; installer downloads `assh_<version>_<os>_<arch>.<archive>` from the matching GitHub release tag and verifies `checksums.txt`; platform mapping matches the GoReleaser matrix; smoke test works without network access.

- [ ] **Step 5: Commit baseline notes only if a documentation correction is needed**

If inspection reveals a documentation contradiction before any command failure, fix only that contradiction and commit:

```bash
git add README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
git commit -m "docs: align release documentation"
```

Expected: skip this step when no documentation contradiction is found.

---

## Task 2: Run CI-Parity Checks

**Files:**
- Modify on failure: Go files under `cmd/` and `internal/`
- Modify on failure: tests under `internal/**/*_test.go`
- Modify on failure: release-facing Markdown files listed by `.github/workflows/ci.yml`

- [ ] **Step 1: Check Go formatting**

Run:

```bash
test -z "$(gofmt -l .)"
```

Expected: PASS with no output. If it fails, run `gofmt -w` on the listed `.go` files, then rerun this exact command.

- [ ] **Step 2: Run Go vet**

Run:

```bash
go vet ./...
```

Expected: PASS. If it fails, fix the reported source or test file and rerun `go vet ./...`.

- [ ] **Step 3: Run Go tests**

Run:

```bash
go test ./...
```

Expected: PASS. If it fails, fix the smallest code or test issue that explains the failing package and rerun `go test ./...`.

- [ ] **Step 4: Run Go race tests**

Run:

```bash
go test -race ./...
```

Expected: PASS. If it fails, fix the reported race or test instability and rerun `go test -race ./...`.

- [ ] **Step 5: Run golangci-lint or fallback**

Run when `golangci-lint` is installed:

```bash
golangci-lint run
```

Fallback when it is not installed:

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
```

Expected: PASS. If it fails, fix reported lint issues without broad refactoring and rerun the same command.

- [ ] **Step 6: Run Markdown lint or fallback**

Run when `markdownlint-cli2` is installed:

```bash
markdownlint-cli2 --config .markdownlint-cli2.yaml README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
```

Fallback when it is not installed:

```bash
npx --yes markdownlint-cli2 --config .markdownlint-cli2.yaml README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
```

Expected: PASS. If it fails, fix only the reported Markdown formatting issues and rerun the same command.

- [ ] **Step 7: Commit CI-parity fixes**

Run after all Task 2 commands pass or are explicitly blocked by unavailable external tooling:

```bash
git status --short
git add cmd internal README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md
git commit -m "fix: resolve ci audit findings"
```

Expected: commit only when Task 2 produced code, test, or doc changes. Skip the commit if there are no changes.

---

## Task 3: Validate Release and npm Packaging

**Files:**
- Modify on failure: `.github/workflows/release.yml`
- Modify on failure: `.goreleaser.yaml`
- Modify on failure: `package.json`
- Modify on failure: `bin/assh.js`
- Modify on failure: `scripts/install.js`
- Modify on failure: `scripts/platform.js`
- Modify on failure: `scripts/smoke-test.js`
- Modify on failure: `scripts/release-contract-test.js`

- [ ] **Step 1: Check package file list includes runtime dependencies**

Run:

```bash
npm pack --dry-run
```

Expected: output includes `bin/assh.js`, `scripts/install.js`, `scripts/platform.js`, `scripts/smoke-test.js`, `README.md`, `README.ru.md`, and `LICENSE`. If any runtime file is missing, update `package.json` `files` and rerun `npm pack --dry-run`.

- [ ] **Step 2: Run npm wrapper smoke test**

Run:

```bash
npm run smoke
```

Expected: PASS and prints `smoke ok`. If it fails, fix `bin/assh.js`, `scripts/platform.js`, or `scripts/smoke-test.js`, then rerun `npm run smoke`.

- [ ] **Step 3: Check GoReleaser config**

Run when `goreleaser` is installed:

```bash
goreleaser check
```

Fallback when it is not installed:

```bash
go run github.com/goreleaser/goreleaser/v2@latest check
```

Expected: PASS. If it fails, fix `.goreleaser.yaml` and rerun the same command.

- [ ] **Step 4: Build local GoReleaser snapshot**

Run when `goreleaser` is installed:

```bash
goreleaser release --snapshot --clean
```

Fallback when it is not installed:

```bash
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

Expected: PASS and creates archives under `dist/` without publishing. If it fails, fix `.goreleaser.yaml` or version ldflags assumptions and rerun the same command.

- [ ] **Step 5: Verify installer archive-name contract against snapshot artifacts**

Run:

```bash
npm run release:contract
```

Expected: PASS and prints `release artifact contract ok`. This script must run GoReleaser snapshot with a temporary local `v<package.json version>` tag when needed, then remove temporary git metadata. If it fails, align `.goreleaser.yaml`, `scripts/install.js`, `scripts/platform.js`, or `scripts/release-contract-test.js`, then rerun Step 4 and Step 5.

- [ ] **Step 6: Verify release tag/version gate syntax locally**

Run:

```bash
GITHUB_REF_NAME="v$(node -p "require('./package.json').version")" zsh -lc 'test "v$(node -p "require('"'./package.json'"').version")" = "$GITHUB_REF_NAME"'
```

Expected: PASS. If it fails, fix the shell quoting in `.github/workflows/release.yml` and rerun an equivalent local command that proves the workflow expression.

- [ ] **Step 7: Commit release packaging fixes**

Run after all Task 3 checks pass:

```bash
git status --short
git add .github/workflows/release.yml .github/workflows/ci.yml .goreleaser.yaml package.json scripts bin/assh.js
git commit -m "fix: resolve release packaging audit findings"
```

Expected: commit only when Task 3 produced changes. Skip the commit if there are no changes.

---

## Task 4: Final Verification and Audit Summary

**Files:**
- Modify if needed: `docs/superpowers/plans/2026-05-15-assh-release-audit.md`

- [ ] **Step 1: Rerun the complete verification set**

Run:

```bash
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go test -race ./...
npm run smoke
npm run release:contract
npm pack --dry-run
```

Expected: all commands PASS.

- [ ] **Step 2: Rerun optional release/lint tools using installed command or fallback**

Run:

```bash
if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run; fi
if command -v markdownlint-cli2 >/dev/null 2>&1; then markdownlint-cli2 --config .markdownlint-cli2.yaml README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md; else npx --yes markdownlint-cli2 --config .markdownlint-cli2.yaml README.md README.ru.md AGENT_INSTRUCTIONS.md SYSTEM_PROMPT_snippet.md; fi
if command -v goreleaser >/dev/null 2>&1; then goreleaser check && goreleaser release --snapshot --clean; else go run github.com/goreleaser/goreleaser/v2@latest check && go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean; fi
```

Expected: all commands PASS or the final report explicitly states the unavailable tool/network blocker.

- [ ] **Step 3: Remove generated release artifacts from the worktree**

Run:

```bash
rm -rf dist
git status --short
```

Expected: `dist/` is not present in `git status`. Remaining changes must be intentional source, config, doc, or plan changes.

- [ ] **Step 4: Produce final audit summary**

Include these points in the final response:

```text
Findings:
- list concrete release, lint, test, or packaging issues found

Fixes:
- list changed files and why

Verification:
- list each command run and whether it passed
- list any command not run and the exact blocker
```

Expected: the user can see whether the tag-driven release flow and npm package path are ready.
