package cli

import (
	"sort"
	"time"

	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/spf13/cobra"
)

func newSessionListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List local sessions",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := session.ListRegistry(stateBaseDir())
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}

			now := time.Now().UTC()
			sort.Slice(entries, func(i, j int) bool {
				if !entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
					return entries[i].CreatedAt.After(entries[j].CreatedAt)
				}
				return entries[i].SID < entries[j].SID
			})

			sessions := make([]response.OK, 0, len(entries))
			for _, entry := range entries {
				expired := (session.Metadata{CreatedAt: entry.CreatedAt, TTLSeconds: entry.TTLSeconds}).Expired(now)
				sessions = append(sessions, response.OK{
					"sid":         entry.SID,
					"session":     entry.Label,
					"host":        entry.Host,
					"user":        entry.User,
					"port":        entry.Port,
					"created_at":  entry.CreatedAt,
					"ttl_seconds": entry.TTLSeconds,
					"seq":         entry.Seq,
					"tmux_name":   entry.TmuxName,
					"expired":     expired,
				})
			}

			return writeJSON(cmd, response.OK{
				"ok":       true,
				"count":    len(sessions),
				"sessions": sessions,
			})
		},
	}

	return cmd
}
