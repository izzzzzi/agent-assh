# assh skill best-practices review design

## Scope

Review and improve every agent-facing surface that teaches agents to use `assh`:

- `skills/assh/SKILL.md`
- `skills/assh/references/*.md`
- `AGENTS.md`, `AGENT_INSTRUCTIONS.md`, `SYSTEM_PROMPT_snippet.md`
- `.cursor/rules/assh.mdc`, `.clinerules/assh.md`, `.github/copilot-instructions.md`
- plugin docs/manifests under `.claude-plugin`, `.codex-plugin`, `.github/plugin`, `.opencode`
- `README.md` and `README.en.md` where they describe skill installation or agent usage

Non-goal: rewrite product docs that explain `assh` to humans unless they conflict with agent-facing instructions.

## Approach

Use eval-driven skill editing, not a full rewrite.

1. Run RED pressure scenarios against the current materials with fresh subagents.
2. Audit the materials against skill-writing best practices: discoverability, concise body, progressive disclosure, concrete commands, safety gates, and no contradictory surfaces.
3. Patch only the smallest set of text needed to fix observed failures and obvious contradictions.
4. Run GREEN pressure scenarios against the updated materials.
5. Run lightweight quality checks: grep for raw SSH guidance, markdown sanity, and git diff review.
6. Commit the final docs/skill changes separately from this design spec.

## Test scenarios

Pressure scenarios should cover:

- raw `ssh`/`scp`/`rsync` temptation
- large remote output/log reading
- password or secret handling
- file transfer and remote file read
- dangerous command confirmation
- session workflow: `connect` → `session exec` → `session read --limit` → `close`

Success means the agent chooses `assh`, uses compact JSON-first commands, paginates output, avoids secret leakage, and asks before dangerous operations.

## Implementation shape

Keep `SKILL.md` concise and action-first. Move heavy details to existing `references/*.md` only when useful. Align other surfaces to the same compact rule set instead of copying the whole skill.

## Risks

- Over-editing docs could create churn. Mitigation: minimal diffs.
- Duplicate surfaces can drift. Mitigation: make short surfaces point to the same core rules and avoid bespoke variants.
- Scenario testing is probabilistic. Mitigation: use multiple focused fresh-context subagents and inspect outputs manually.
