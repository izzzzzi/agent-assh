package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/spf13/cobra"
)

func newSessionExportCommand() *cobra.Command {
	var sid string
	var output string
	cmd := &cobra.Command{
		Use:           "export",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if output == "" {
				output = filepath.Join(stateBaseDir(), "exports", sid+".tar.gz")
			}
			result, err := session.Export(stateBaseDir(), sid, output)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return writeError(cmd, "session_not_found", err.Error(), "")
				}
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			return writeJSON(cmd, result)
		},
	}
	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVarP(&output, "output", "o", "", "archive output path")
	return cmd
}
