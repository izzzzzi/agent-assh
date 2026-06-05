package cli

import (
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/spf13/cobra"
)

func newSessionWatchCommand() *cobra.Command {
	var sid string

	cmd := &cobra.Command{
		Use:           "watch",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			return writeJSON(cmd, map[string]any{
				"ok":          true,
				"sid":         sid,
				"session":     entry.Label,
				"host":        entry.Host,
				"user":        entry.User,
				"tmux_name":   entry.TmuxName,
				"attach_cmd":  "ssh " + entry.User + "@" + entry.Host + " -t 'tmux attach -t " + entry.TmuxName + "'",
				"watch_cmd":   "ssh " + entry.User + "@" + entry.Host + " -t 'tmux attach -t " + entry.TmuxName + "'",
				"description": "Run the attach_cmd in a separate terminal to watch the agent's tmux session in real-time.",
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	return cmd
}
