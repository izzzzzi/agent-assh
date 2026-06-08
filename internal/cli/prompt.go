package cli

import (
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/spf13/cobra"
)

const agentPrompt = `Use ` + "`assh`" + ` for SSH work.

Connect with direct host + key (simplest — no alias, no password):
assh connect -H HOST -u root -i KEY -n NAME

For ~/.ssh/config aliases:
assh connect --ssh-config ALIAS -n NAME

For picky SSH gateways (RunPod, etc.) that reject -T:
assh connect -H HOST -u root -i KEY --force-pty -n NAME
assh exec -H HOST -u root -i KEY --force-pty -- "command"

For pasted provider server-info, save to a 0600 temp file, run:
assh connect-info --file TMP -n NAME
Then remove TMP. If parsing fails, extract host/user/password manually and use assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME.

Never put passwords in command arguments. Never print, log, repeat, or summarize passwords.

Use the returned sid and next_commands for all remote work:
assh session exec -s SID -- "pwd"
assh session read -s SID --seq 1 --limit 50
assh session read -s SID --seq 1 --stream stderr --limit 50
assh session read -s SID --seq 1 --limit 50 --raw
assh session list
assh session export -s SID --output session.tar.gz
assh session close -s SID
assh session watch -s SID

Server management:
assh scan -H HOST -u USER
assh session ps -s SID --top 20
assh session kill -s SID --pid PID
assh session service -s SID --action status --service nginx
assh session service -s SID --action logs --service nginx --lines 100

File operations:
assh transfer list -H HOST -u USER --path /var/log
assh transfer stat -H HOST -u USER --path /etc/nginx.conf
assh transfer put -H HOST LOCAL_PATH REMOTE_PATH
assh transfer get -H HOST REMOTE_PATH LOCAL_PATH
assh transfer sync --direction push --source ./dist --dest /var/www -H HOST -u USER
assh transfer mkdir -H HOST -u USER --path /opt/app
assh transfer rm -H HOST -u USER --path /tmp/junk
assh transfer mv -H HOST -u USER --source /tmp/a --dest /tmp/b

Background jobs:
assh session exec-async -s SID -- "long-build.sh"
assh session job-status -s SID --job-id JOB_ID
assh session job-status -s SID --job-id JOB_ID --raw  # bare output, no JSON
assh session job-cancel -s SID --job-id JOB_ID

Docker:
assh session docker-ps -s SID
assh session docker-logs -s SID --container NAME
assh session docker-exec -s SID --container NAME -- "ls -la"

Database (read-only — only SELECT/SHOW/DESCRIBE/EXPLAIN):
assh session db-query -s SID --type mysql -d DB -q "SELECT ..."

Fleet (multi-host parallel):
assh fleet exec -H host1 -H host2 -u root -- "uptime"

Pre/post hooks:
assh session exec -s SID --before "git stash" --after "git stash pop" -- "deploy.sh"

If session exec returns dangerous_command_requires_confirmation, do not add --confirm-danger unless the user explicitly intended the destructive action.
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
			"Do not add --confirm-danger unless the user explicitly intended the destructive action.",
			"db-query is read-only — only SELECT/SHOW/DESCRIBE/EXPLAIN queries allowed.",
			"Use assh session watch to observe agent actions in real-time.",
		},
		"workflow": []string{
			"Prefer assh connect --ssh-config ALIAS for hosts in ~/.ssh/config.",
			"For pasted provider server-info, write the full block to a mode 0600 temporary file.",
			"Run assh connect-info --file TMP -n NAME, then remove TMP.",
			"If parsing fails, extract host, user, and password; put the password in an environment variable.",
			"Run assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME.",
			"Continue with returned sid and next_commands.",
			"Use assh session exec and assh session read with explicit limits.",
		},
		"commands": response.OK{
			"prompt":           "assh prompt",
			"connect_info":     "assh connect-info --file TMP -n NAME",
			"connect":          "assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME",
			"connect_key":      "assh connect -H HOST -u USER -i KEY -n NAME",
			"connect_ssh_conf": "assh connect --ssh-config ALIAS -n NAME",

			"session_exec":     "assh session exec -s SID -- \"pwd\"",
			"session_read":     "assh session read -s SID --seq 1 --limit 50",
			"session_list":     "assh session list",
			"session_export":   "assh session export -s SID --output session.tar.gz",
			"session_close":    "assh session close -s SID",
			"session_watch":    "assh session watch -s SID",
			"scan":             "assh scan -H HOST -u USER",
			"session_ps":       "assh session ps -s SID --top 20",
			"session_service":  "assh session service -s SID --action status --service nginx",
			"transfer_list":    "assh transfer list -H HOST -u USER --path /var/log",
			"transfer_put":     "assh transfer put -H HOST LOCAL_PATH REMOTE_PATH",
			"transfer_get":     "assh transfer get -H HOST REMOTE_PATH LOCAL_PATH",
			"transfer_sync":    "assh transfer sync --direction push --source ./dist --dest /var/www -H HOST",
			"session_async":    "assh session exec-async -s SID -- \"long-build.sh\"",
			"session_docker":   "assh session docker-ps -s SID",
			"session_db_query": "assh session db-query -s SID --type mysql -d DB -q \"SELECT ...\"",
			"fleet_exec":       "assh fleet exec -H host1 -H host2 -u root -- \"uptime\"",
			"forward":          "assh forward status --name NAME",
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
