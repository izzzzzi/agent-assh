<!-- /autoplan restore point: /Users/apple/.gstack/projects/izzzzzi-agent-assh/main-autoplan-restore-20260615-132303.md -->
# Plan: Close Top Competitive Gaps for assh

Branch: main | Repo: izzzzzi/agent-assh | Commit: 5b27869
Drafted by /autoplan from competitive analysis (2026-06-15)
Status: APPROVED by /autoplan on 2026-06-15 — option A (all recommendations accepted)
Reviews: CEO + Eng + DX (subagent-only; Codex unavailable — usage limit). All
critical findings independently code-verified against the repo.

## Problem Statement

`assh` is a Go CLI that gives LLM agents token-efficient, safety-gated SSH access
over the system OpenSSH client with persistent tmux sessions. Competitive analysis
against MCP-SSH servers and token-savers surfaced four candidate gaps. Review
corrected the premises and re-scoped each feature. assh stays CLI-only by design —
the CLI is the deliberate alternative to MCP servers, not a gap to close.

## Premises (post-review)

1. Remote command stdout can contain secrets; today it flows to the agent context
   AND to the local output/transcript store verbatim. Redaction is a **best-effort
   hygiene filter, NOT a security boundary** (regex misses the long tail; the secret
   already transited the transport).
2. Agents can already read remote files via `cat` (exec/session). A dedicated
   command is **token-noise reduction**, not new capability.
3. The safety classifier is compiled in; operators may want to **add** deny rules
   without rebuilding. Relaxing built-in rules via a file is rejected (security).
4. assh's token economy is unmeasured; a line-withheld counter makes it visible.
   Metric is **lines**, explicitly not tokens.

## Scope — Features (corrected)

### F1: Secret redaction in command output — best-effort hygiene (HIGH ROI)
- **What:** filter known secret patterns (AWS keys, bearer tokens, PEM blocks,
  `password=`/`token=` assignments, JWTs) → `[REDACTED:type]`. **Labeled hygiene,
  not a security guarantee** in all docs.
- **Where (CORRECTED — redact at WRITE time, not serve time):**
  - exec: redact `result.Stdout`/`Stderr` in `internal/cli/exec.go:newExecCommand`
    **before** `store.Write(outputID, ...)` (exec.go:29). This also closes the
    plaintext-at-rest gap in `outputs/<id>`.
  - session: redact `content` in `internal/cli/session.go:newSessionReadCommand`
    once, **after** `parseSessionRead`, before both `SessionOutputStore.Write` and
    the transcript `Append`.
  - Redactor MUST be line-count-preserving (inline `[REDACTED:type]`); reject any
    pattern that adds/removes newlines, so `OutputStore.Read` total/offset/has_more
    stay consistent across pages (output.go computes these on stored lines).
  - Precompile patterns at package init (`regexp.MustCompile`), not per call.
- **JSON contract (NEW):** add `redacted:bool` + `redaction_count:int` to the read
  responses (`OutputPage` and session read). Agent contract: `[REDACTED:type]` is
  intentional; the command succeeded; **do not retry to recover the value.**
- **Flag:** `--no-redact` (default off = redaction on). No `--no-*` precedent, but
  consistent with `--confirm-danger` boolean style. Acceptable.
- **Consistency:** apply redaction across all stdout-serving paths or explicitly
  list covered ones. Covered v1: `exec`/`read`, `session read`. Documented as such.
- **Out of scope:** redacting the local audit log (already hash-only).

### F2: `transfer read` — remote file read over ssh (HIGH ROI)
- **CORRECTION:** the original "reuse existing SFTP transport / SFTP get exists" is
  **false**. There is no SFTP anywhere: `transfer get` uses **scp**
  (`transfer.go:newTransferGetCommand → transport.Download → SCPDirection → scp`
  binary in `internal/transport/scp.go`). `transfer_sftp.go` is plain ssh running
  `find`/`stat`/etc.
- **What:** `assh transfer read -H HOST -u USER --path P` (single name — drop
  `session read-file`/`cat`). Mirrors `read`/`session read`.
- **Where (real path, LOW effort):** mirror `remoteFileListCommand` pattern in
  `transfer_sftp.go` — run `cat`/`sed -n`/`head -c` over `runSSH`, store into
  `OutputStore`, reuse the `read` pagination contract (`OutputPage`: same field
  names so the agent's paging logic is unchanged). No new transport, no pkg/sftp.
- **Guards + typed errors (populate the unused `hint` field):**
  - missing → `remote_file_not_found`, hint `assh transfer stat ...`
  - directory → `not_a_file`, hint `assh transfer list ...`
  - oversized (define explicit byte limit) → `file_too_large`, hint use `--offset/--limit` or `transfer get`
  - binary (NUL detection) → `binary_file`, hint use `transfer get`
  - perms → `permission_denied`, hint `assh transfer stat ...`
- **Redaction:** F1 applies here too; `redacted` flag fires so the agent knows file
  content is not verbatim.
- **Out of scope:** binary dump, write path (covered by `transfer put`).

### F3: Declarative safety policy — additive deny-only (MEDIUM)
- **CORRECTION:** drop the `allow:` relaxation entirely. `exec` has **no**
  `--confirm-danger` by design (exec.go); an `allow:` line in a dotfile is an
  unaudited bypass of the only guardrail. Additive deny rules only.
- **What:** optional `~/.config/assh/safety.rules` that **adds** deny rules over the
  built-ins in `internal/safety/safety.go`. Default behavior identical when absent.
- **Grammar (must be defined, compose with the tokenizer):** rules match against the
  tokenized command name/args used by `CheckCommand` (NOT raw-string match, which is
  evadable via `sudo`/`sh -c`/substitution that the engine already unwraps). Specify
  deny semantics precisely; built-in deny ∪ file deny (union, deterministic).
- **Hardening + DX:**
  - reject file unless `0600` and user-owned → `safety_policy_invalid`, hint
    `chmod 600 ~/.config/assh/safety.rules`.
  - malformed line → `safety_policy_parse_error` with line number + expected syntax.
  - decide fail-closed on invalid file (refuse), documented explicitly.
  - record policy file path + content hash in the audit event when loaded.
  - surface matched rule **source** (`builtin` | `policy:line N`) in
    `safety.Result` / error message.
  - introduce NO new error codes the agent doesn't already handle for the
    confirm-danger path (keep AGENT_INSTRUCTIONS Security flow stable).
- **Out of scope:** disabling the classifier; relaxing built-in rules.

### F4: Output-withheld counter + `audit --savings` (MEDIUM)
- **What:** record raw vs served line counts per read; `assh audit --savings`
  summarizes withheld output. Metric is **lines**, labeled as such — not tokens.
- **Where:**
  - add `RawLines`/`ServedLines` to `internal/audit/audit.go:Event`,
    **non-omitempty** (omitempty drops legitimate zeros and skews aggregation).
  - **NEW behavior:** `internal/cli/exec.go:newReadCommand` writes NO audit event
    today — add one. `raw_lines = page.TotalLines`, `served_lines = countLines(page.Content)`.
  - session read: use `total` from `parseSessionRead` and `countLines(content)` —
    **not** the existing `countLines(result.Stdout)` which includes the
    `__ASSH_TOTAL_LINES__` marker line (off-by-one).
  - `audit --savings` returns a single aggregate object
    `{ok:true, raw_lines, served_lines, withheld_lines}` and **bypasses the
    `--last >= 1` guard** (misc.go:105 hard-fails `last<1` and returns an array —
    different shape). Define the summary schema explicitly.
- **Naming:** field is `withheld_lines`, not `tokens_saved`. Not surfaced in
  `next_commands` (operator/audit-facing, keeps the agent hot path clean).
- **Redaction interaction:** define whether redacted lines count as served (they do
  — they are still emitted, just masked).
- **Out of scope:** byte/token estimation; remote telemetry.

## NOT in scope
- **MCP-server facade — permanently out of scope (product stance).** assh is
  CLI-only by deliberate design: agents drive it via CLI commands returning
  metadata-first JSON. The MCP server was built and removed in `029e554`
  ("contradicts CLI philosophy"). The CLI is the *alternative* to MCP servers, not a
  gap to close. Rejected by product decision (logged 2026-06-15). Do not re-litigate.
- tok-style per-command output compression — deferred.
- Host inventory file for `fleet` — deferred.
- Snapshot/ephemeral environment provisioning — deferred.

## What already exists (mapped)
- `assh exec -- "cat file"` already reads remote files (F2 = token-noise reduction).
- `internal/safety/safety.go` already does shell-aware destructive parsing with
  sudo/env/sh -c unwrap (F3 must only add, never weaken).
- `audit.Event` already has StdoutLines/StderrLines (F4 fields are consistent).
- `response.Error.Hint` exists but is `""` in 50/51 calls (F2/F3 must populate it).

## Test Plan (concrete)
- **F1:** golden tests for a secret straddling a page boundary, multi-line PEM,
  line-count preservation, and a false-positive corpus (git SHAs, base64 logs,
  `token=` in URLs). Verify `redacted`/`redaction_count` in JSON.
- **F2:** cat path with pagination; binary + oversized guards; missing/dir/perms
  typed errors with hints.
- **F3:** overlay precedence (union); malformed + wrong-perms rejection;
  evasion attempts (`sudo`/`sh -c`/substitution wrapping); audit hash recorded;
  rule-source in message.
- **F4:** aggregation with the new exec-read audit event; non-omitempty zeros;
  marker-line off-by-one avoided; `--savings` bypasses `--last>=1`.
- Table-driven, matching existing `*_test.go` style.

## Docs / prompt updates (required, not just README Security)
- **AGENT_INSTRUCTIONS.md:** add `transfer read` to File Operations; add redaction
  note to Context Discipline + Security Rules (`[REDACTED:type]` is intentional,
  do not retry); document F2 typed errors.
- **SYSTEM_PROMPT_snippet.md:** add `transfer read` to file-ops block; one-line
  redaction note in the JSON Contract section.
- **README.md + README.en.md (keep ru/en in sync):** Commands, Security, and Token
  Economy (F4) sections.

## Rollout
- Independent, shippable features. Order: **F1** (security hygiene, highest risk —
  build the redact-at-write wiring correctly first) → **F2** → **F4** (cheap, sound)
  → **F3** (needs grammar + hardening design). F4 may ship before F3 if F3 design
  lands later.

---
<!-- AUTONOMOUS DECISION LOG -->
## Decision Audit Trail

| # | Phase | Decision | Classification | Principle | Rationale | Rejected |
|---|-------|----------|----------------|-----------|-----------|----------|
| 1 | CEO | Keep problem domain (safe+cheap remote ops for agents) | Mechanical | P1 | Right problem, confirmed both angles | Reframe entirely |
| 2 | CEO | Disclose MCP removal (029e554) in plan | Mechanical | P5 | Deliberate stance, not hidden failure | Stay silent |
| 3 | CEO | MCP facade permanently out of scope (CLI = alternative) | User decision | — | Confirmed by user 2026-06-15 + prior team call | Build MCP facade |
| 4 | Eng/CEO | F2: replace false "SFTP get exists" with cat-over-ssh | Mechanical | P5 | Code proves transfer get = scp; no SFTP exists | Add pkg/sftp dep |
| 5 | Eng | F1: redact at WRITE time, not serve time | Mechanical | P5 | Serve-time breaks pagination + page-boundary leak | Redact on serve |
| 6 | Eng | F1: also redact exec raw store (exec.go:29) | Mechanical | P1 | Otherwise plaintext secrets persist on disk | Leave raw at rest |
| 7 | Eng | F3: drop `allow:`; additive deny-only | Mechanical | P4 | exec has no --confirm-danger by design | Keep allow: |
| 8 | Eng | F4: add NEW audit event to exec-read | Mechanical | P1 | exec.go:newReadCommand has no writeAudit | Assume exists |
| 9 | Eng | F4: count via parseSessionRead total (marker off-by-one) | Mechanical | P5 | Existing count includes marker line | Reuse existing count |
| 10 | DX | F1: add `redacted`+count to JSON; agent contract doc | Mechanical | P1 | Silent redaction makes agents loop | Redact silently |
| 11 | DX | F2: single name `transfer read` | Taste→accepted A | P5 | Mirrors read/session read; one name | Ship both names |
| 12 | DX | F2/F3: typed error codes + populate `hint` | Mechanical | P1 | hint is "" in 50/51 calls | Generic errors |
| 13 | DX | F4: `--savings` aggregate object, bypass --last guard | Mechanical | P5 | misc.go:105 hard-fails last<1 | Reuse --last path |
| 14 | DX | F4: metric `withheld_lines`, non-omitempty | Mechanical | P5 | "savings" misleads; omitempty drops zeros | Call it token savings |
| 15 | CEO/Eng | F1: best-effort hygiene, NOT security boundary | Taste→accepted A | P5 | Regex misses long tail; secret already transited | Market as leak fix |

## Cross-Phase Themes
- **F1 mis-architected (Eng) + under-specified for agents (DX)** — redact-at-write +
  `redacted` JSON flag fixes both. High-confidence signal.
- **F2 "SFTP" premise false** — CEO framing + Eng code proof converge. cat-over-ssh.
- **F3 security regression as written** — Eng + DX both blocker. Additive-deny only.
