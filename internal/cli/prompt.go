package cli

import (
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/spf13/cobra"
)

const agentPrompt = `Use ` + "`assh`" + ` for SSH work.

If the user pasted a provider server-info block, save the full block to a mode 0600 temporary file, run:
assh connect-info --file TMP -n NAME
Then remove TMP.

If connect-info cannot parse the block, extract host, user, and password yourself. Put the password in an environment variable and run:
assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME

Never put passwords in command arguments. Never print, log, repeat, or summarize passwords.

Use the returned sid and next_commands for all remote work:
assh session exec -s SID -- "pwd"
assh session read -s SID --seq 1 --limit 50
assh session read -s SID --seq 1 --stream stderr --limit 50
assh session read -s SID --seq 1 --limit 50 --raw
assh session list
assh transfer put -H HOST LOCAL_PATH REMOTE_PATH
assh transfer get -H HOST REMOTE_PATH LOCAL_PATH
assh forward status --name NAME
assh session close -s SID

Keep large remote output out of context. Read bounded windows with --limit, --offset, and --stream. Use --raw only for piping or exact output.
`

func agentHelpManifest() response.OK {
	return response.OK{
		"ok":                   true,
		"tool":                 "assh",
		"version":              version,
		"audience":             "llm_agent",
		"purpose":              "SSH workflow helper for LLM agents",
		"agent_prompt_command": "assh prompt",
		"docs": []string{
			"AGENT_INSTRUCTIONS.md",
			"SYSTEM_PROMPT_snippet.md",
		},
		"agent_prompt": agentPrompt,
		"safety_rules": []string{
			"Never put passwords in command arguments.",
			"Prefer connect-info --file for pasted provider server-info blocks.",
			"Remove temporary server-info files after connect.",
			"Use returned sid and next_commands for remote work.",
			"Read large output with bounded session read windows.",
		},
		"workflow": []string{
			"For pasted provider server-info, write the full block to a mode 0600 temporary file.",
			"Run assh connect-info --file TMP -n NAME, then remove TMP.",
			"If parsing fails, extract host, user, and password; put the password in an environment variable.",
			"Run assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME.",
			"Continue with returned sid and next_commands.",
			"Use assh session exec and assh session read with explicit limits.",
		},
		"commands": response.OK{
			"prompt":        "assh prompt",
			"connect_info":  "assh connect-info --file TMP -n NAME",
			"connect":       "assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME",
			"session_exec":  "assh session exec -s SID -- \"pwd\"",
			"session_read":  "assh session read -s SID --seq 1 --limit 50",
			"session_list":  "assh session list",
			"transfer_put":  "assh transfer put -H HOST LOCAL_PATH REMOTE_PATH",
			"transfer_get":  "assh transfer get -H HOST REMOTE_PATH LOCAL_PATH",
			"forward":       "assh forward status --name NAME",
			"session_close": "assh session close -s SID",
		},
		"json_contract": response.OK{
			"operational_commands_emit_json": true,
			"raw_read_commands_emit_content": true,
			"errors_use_ok_false":            true,
		},
	}
}

func writeAgentHelp(cmd *cobra.Command) {
	if err := writeJSON(cmd, agentHelpManifest()); err != nil {
		_, _ = cmd.ErrOrStderr().Write([]byte("failed to write agent help: " + err.Error() + "\n"))
	}
}

func newPromptCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "prompt",
		Short:         "Print minimal LLM-agent usage instructions",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := cmd.OutOrStdout().Write([]byte(agentPrompt))
			return err
		},
	}
}
