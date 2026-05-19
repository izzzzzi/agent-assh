# Agent Prompt Help Design

## Goal

Make `assh --help` useful to LLM agents that discover the CLI through help output. The help should point agents to a compact usage prompt without embedding a long instruction block in every help response.

## User Experience

`assh --help` will include a short `Agent prompts` section. The section will tell agents to run `assh prompt` for the minimal SSH workflow instruction and mention the packaged markdown prompt files:

- `AGENT_INSTRUCTIONS.md`
- `SYSTEM_PROMPT_snippet.md`

The help output will not promise a hard-coded absolute path, because npm global install locations vary by environment.

`assh prompt` will print a concise plain-text prompt suitable for copying into, or being read by, a terminal agent. It will cover:

- use `assh` for SSH work;
- prefer `assh connect-info --file TMP -n NAME` for pasted provider server-info blocks;
- store pasted server-info in a mode `0600` temporary file and remove it after connect;
- never put passwords in command arguments or responses;
- use `assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME` when manual extraction is needed;
- continue through the returned `sid` and `next_commands`;
- use `assh session exec/read` with bounded reads instead of dumping large output.

## Architecture

Add a root-level Cobra command named `prompt`. It will be read-only, require no positional arguments, and write plain text to stdout. It will not use the JSON response contract because the command is documentation-oriented rather than an operational action.

Customize the root help text to include the `Agent prompts` section while preserving the standard Cobra command and flag listing. The section should be short and stable so ordinary human help remains readable.

Include `AGENT_INSTRUCTIONS.md` and `SYSTEM_PROMPT_snippet.md` in `package.json` `files`, so the referenced prompt files are present in npm packages.

## Data Flow

For discovery:

1. Agent runs `assh --help`.
2. Help output shows the `Agent prompts` section.
3. Agent runs `assh prompt`.
4. CLI prints the minimal prompt to stdout.
5. Agent follows the prompt for connect, session exec, session read, and cleanup.

For package installation:

1. `npm pack` includes the existing markdown prompt files.
2. Installed packages have both the binary wrapper and the prompt reference files.

## Error Handling

`assh prompt` rejects positional arguments with the existing JSON invalid-args helper, matching other commands. Normal prompt output is plain text.

The root help should not fail if markdown files are absent in a source checkout or custom install. It only mentions file names and the `assh prompt` command, so help generation has no filesystem dependency.

## Testing

Add focused tests for:

- root help contains `Agent prompts`;
- root help mentions `assh prompt`;
- `assh prompt` prints the key SSH workflow rules;
- `assh prompt extra` returns an invalid-args error;
- package dry-run or release contract covers inclusion of `AGENT_INSTRUCTIONS.md` and `SYSTEM_PROMPT_snippet.md`.

Existing operational command tests should continue to prove JSON behavior is unchanged for `connect`, `session`, `exec`, and related commands.

## Scope

In scope:

- short root help discovery section;
- `assh prompt` command;
- npm package file inclusion;
- tests for help, prompt output, and packaging.

Out of scope:

- embedding the full agent instructions in `assh --help`;
- promising environment-specific absolute prompt paths;
- multiple prompt formats or flags such as `--json`, `--paths`, or `--full`;
- changing operational command JSON contracts.
