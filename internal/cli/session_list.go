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
		Aliases:       []string{"ls"},
		Short:         "List local sessions",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE:          runSessionList,
	}

	return cmd
}

// newSessionListRootAlias exposes `assh ls` and `assh list` at the top level.
// Sessions are the thing callers look for most often, and transcripts show
// agents reaching for `assh ls` rather than `assh session list`. It is hidden
// because it is discoverability, not a second command.
func newSessionListRootAlias() *cobra.Command {
	cmd := newSessionListCommand()
	cmd.Use = "ls"
	cmd.Aliases = []string{"list"}
	cmd.Hidden = true
	cmd.Short = "List local sessions (alias for `assh session list`)"
	return cmd
}

func runSessionList(cmd *cobra.Command, args []string) error {
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
			"sid":          entry.SID,
			"session":      entry.Label,
			"host":         entry.Host,
			"user":         entry.User,
			"port":         entry.Port,
			"created_at":   entry.CreatedAt,
			"ttl_seconds":  entry.TTLSeconds,
			"seq":          entry.Seq,
			"tmux_name":    entry.TmuxName,
			"expired":      expired,
			"status_basis": "ttl_only",
			"reachable":    "unknown",
		})
	}

	return writeJSON(cmd, response.OK{
		"ok":       true,
		"count":    len(sessions),
		"sessions": sessions,
	})
}
