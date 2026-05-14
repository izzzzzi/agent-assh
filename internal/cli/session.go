package cli

import (
	"github.com/agent-ssh/assh/internal/response"
	"github.com/spf13/cobra"
)

func newSessionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "session",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(newSessionOpenCommand(), newSessionCloseCommand(), newSessionGCCommand())
	return cmd
}

func newSessionOpenCommand() *cobra.Command {
	var host string
	var label string
	var installTmux bool

	cmd := &cobra.Command{
		Use:           "open",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				return writeInvalidArgs(cmd, "host required", "")
			}

			return writeJSON(cmd, response.OK{
				"ok":           true,
				"operation":    "session_open",
				"host":         host,
				"label":        label,
				"install_tmux": installTmux,
			})
		},
	}

	cmd.Flags().StringVarP(&host, "host", "H", "", "SSH host")
	cmd.Flags().StringVarP(&label, "name", "n", "", "session label")
	cmd.Flags().BoolVar(&installTmux, "install-tmux", false, "install tmux if missing")
	return cmd
}

func newSessionCloseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "close",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "--sid is required", "")
		},
	}
	return cmd
}

func newSessionGCCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "gc",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeJSON(cmd, response.OK{
				"ok":         true,
				"dry_run":    true,
				"candidates": []string{},
			})
		},
	}
	return cmd
}
