package cli

import (
	"github.com/agent-ssh/assh/internal/remote"
	"github.com/agent-ssh/assh/internal/response"
	"github.com/spf13/cobra"
)

func newSessionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "session",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(
		newSessionOpenCommand(),
		newSessionExecCommand(),
		newSessionReadCommand(),
		newSessionCloseCommand(),
		newSessionGCCommand(),
	)
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
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
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

func newSessionExecCommand() *cobra.Command {
	var sid string

	cmd := &cobra.Command{
		Use:           "exec -- command",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if len(args) == 0 {
				return writeInvalidArgs(cmd, "command required", "")
			}

			return writeJSON(cmd, response.OK{
				"ok":        true,
				"operation": "session_exec",
				"sid":       sid,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	return cmd
}

func newSessionReadCommand() *cobra.Command {
	var sid string
	var seq int
	var stream string
	var offset int
	var limit int

	cmd := &cobra.Command{
		Use:           "read",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if seq < 1 {
				return writeInvalidArgs(cmd, "--seq is required", "")
			}
			if stream != "stdout" && stream != "stderr" {
				return writeInvalidArgs(cmd, "invalid stream", "")
			}
			if offset < 0 || limit < 1 {
				return writeInvalidArgs(cmd, "invalid pagination", "")
			}

			return writeJSON(cmd, response.OK{
				"ok":        true,
				"operation": "session_read",
				"sid":       sid,
				"seq":       seq,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().IntVar(&seq, "seq", 0, "session command sequence")
	cmd.Flags().StringVar(&stream, "stream", "stdout", "stdout|stderr")
	cmd.Flags().IntVar(&offset, "offset", 0, "line offset")
	cmd.Flags().IntVar(&limit, "limit", 50, "line limit")
	return cmd
}

func newSessionCloseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "close",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
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
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
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
