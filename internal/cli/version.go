package cli

import (
	"runtime"

	"github.com/agent-ssh/assh/internal/response"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print version information as JSON",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeJSON(cmd, response.OK{
				"ok":         true,
				"version":    version,
				"commit":     commit,
				"date":       date,
				"go_version": runtime.Version(),
			})
		},
	}
}
