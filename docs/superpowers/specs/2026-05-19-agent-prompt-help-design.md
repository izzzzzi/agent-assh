# Agent Prompt Help Design

## Goal

Make `assh --help` useful to LLM agents that discover the CLI through help output. Because `assh` is an agent-first tool, `--help` should be a machine-readable discovery contract rather than a standard human-oriented Cobra help page.

## User Experience

`assh --help` will return a JSON manifest. The manifest will identify `assh` as an LLM-agent SSH workflow helper and include:

- the tool name, version, audience, and purpose;
- the command to print the plain-text prompt: `assh prompt`;
- the packaged markdown prompt files;
- a compact `agent_prompt` string;
- safety rules;
- canonical workflow steps;
- key commands and examples;
- notes about JSON output and bounded reads.

The manifest will mention these packaged markdown prompt files:

- `AGENT_INSTRUCTIONS.md`
- `SYSTEM_PROMPT_snippet.md`

The manifest will not promise a hard-coded absolute path, because npm global install locations vary by environment. Human-facing documentation remains in `README.md` and `README.en.md`.

`assh prompt` will print a concise plain-text prompt suitable for copying into, or being read by, a terminal agent. It will cover the same behavioral rules as the manifest in a form that reads like direct instructions:

- use `assh` for SSH work;
- prefer `assh connect-info --file TMP -n NAME` for pasted provider server-info blocks;
- store pasted server-info in a mode `0600` temporary file and remove it after connect;
- never put passwords in command arguments or responses;
- use `assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME` when manual extraction is needed;
- continue through the returned `sid` and `next_commands`;
- use `assh session exec/read` with bounded reads instead of dumping large output.

## Architecture

Override root help behavior so `assh --help` and `assh help` write the JSON manifest to stdout. The output must be valid JSON and follow the existing response style with `"ok": true`.

Add a root-level Cobra command named `prompt`. It will be read-only, require no positional arguments, and write plain text to stdout. It will not use the JSON response contract for successful output because the command is documentation-oriented companion text, not an operational action.

Cobra's standard command and flag listing will no longer be the root help experience. README files are the human help surface. The JSON manifest is the agent discovery surface.

Include `AGENT_INSTRUCTIONS.md` and `SYSTEM_PROMPT_snippet.md` in `package.json` `files`, so the referenced prompt files are present in npm packages.

## Data Flow

For discovery:

1. Agent runs `assh --help`.
2. CLI returns a JSON manifest.
3. Agent reads `agent_prompt`, `workflow`, `safety_rules`, and command examples.
4. Agent optionally runs `assh prompt` for direct plain-text instructions.
5. Agent follows the prompt for connect, session exec, session read, and cleanup.

For package installation:

1. `npm pack` includes the existing markdown prompt files.
2. Installed packages have both the binary wrapper and the prompt reference files.

## Error Handling

`assh prompt` rejects positional arguments with the existing JSON invalid-args helper, matching other commands. Normal prompt output is plain text.

Root help generation should not read prompt files at runtime, so `assh --help` cannot fail because markdown files are absent in a source checkout or custom install. The manifest only mentions file names, command examples, and inline prompt content.

If Cobra receives invalid help usage, errors should continue to use the existing JSON error shape.

## Testing

Add focused tests for:

- `assh --help` returns valid JSON;
- root help JSON includes `"ok": true`, `"audience": "llm_agent"`, and `agent_prompt_command`;
- root help JSON includes `assh prompt`, `connect-info`, `session exec`, and `session read`;
- `assh prompt` prints the key SSH workflow rules;
- `assh prompt extra` returns an invalid-args error;
- package dry-run or release contract covers inclusion of `AGENT_INSTRUCTIONS.md` and `SYSTEM_PROMPT_snippet.md`.

Existing operational command tests should continue to prove JSON behavior is unchanged for `connect`, `session`, `exec`, and related commands.

## Scope

In scope:

- JSON manifest output for `assh --help`;
- `assh prompt` command;
- npm package file inclusion;
- tests for help, prompt output, and packaging.

Out of scope:

- standard human-oriented Cobra root help;
- reading markdown prompt files at runtime for root help;
- promising environment-specific absolute prompt paths;
- multiple prompt formats or flags such as `--json`, `--paths`, or `--full`;
- changing operational command JSON contracts.
