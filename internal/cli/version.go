package cli

import (
	"runtime"

	"github.com/izzzzzi/agent-assh/internal/response"
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
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
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
