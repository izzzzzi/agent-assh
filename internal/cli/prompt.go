package cli

import (
	_ "embed"

	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/spf13/cobra"
)

// agentPrompt is generated from skills/assh/SKILL.md by
// scripts/check-skill-copies.js, so `assh prompt` cannot drift from the skill.
//
//go:embed agent_prompt.md
var agentPrompt string

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
		"commands": response.OK{
			"prompt":           "assh prompt",
			"connect_info":     "assh connect-info --file TMP -n NAME",
			"connect":          "assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME",
			"connect_key":      "assh connect -H HOST -u USER -i KEY -n NAME",
			"connect_ssh_conf": "assh connect --ssh-config ALIAS -n NAME",

			"session_exec":    "assh session exec -s SID -- \"pwd\"",
			"session_read":    "assh session read -s SID --seq 1 --limit 50",
			"session_list":    "assh session list",
			"session_export":  "assh session export -s SID --output session.tar.gz",
			"session_close":   "assh session close -s SID",
			"session_watch":   "assh session watch -s SID",
			"scan":            "assh scan -H HOST -u USER",
			"session_ps":      "assh session ps -s SID --top 20",
			"session_service": "assh session service -s SID --action status --service nginx",
			"transfer_list":   "assh transfer list -H HOST -u USER --path /var/log",
			"transfer_put":    "assh transfer put -H HOST LOCAL_PATH REMOTE_PATH",
			"transfer_get":    "assh transfer get -H HOST REMOTE_PATH LOCAL_PATH",
			"transfer_read":   "assh transfer read -H HOST -u USER --path /etc/app.conf",
			"audit_savings":   "assh audit --savings",
			"transfer_sync":   "assh transfer sync --direction push --source ./dist --dest /var/www -H HOST",
			"session_async":   "assh session exec-async -s SID -- \"long-build.sh\"",
			"fleet_exec":      "assh fleet exec -H host1 -H host2 -u root -- \"uptime\"",
			"forward":         "assh forward status --name NAME",
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
