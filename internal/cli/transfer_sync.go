package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/spf13/cobra"
)

func newTransferSyncCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var direction string
	var source string
	var dest string
	var delete bool
	var excludes []string

	cmd := &cobra.Command{
		Use:           "sync",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if source == "" || dest == "" {
				return writeInvalidArgs(cmd, "--source and --dest are required", "")
			}
			if direction != "push" && direction != "pull" {
				return writeInvalidArgs(cmd, "--direction must be push or pull", "")
			}

			rsyncBinary := "rsync"
			rsyncArgs := []string{"-az", "--info=progress2"}

			if delete {
				rsyncArgs = append(rsyncArgs, "--delete")
			}
			for _, ex := range excludes {
				rsyncArgs = append(rsyncArgs, "--exclude="+ex)
			}

			// SSH options for rsync
			sshTarget := ssh.User + "@" + ssh.Host
			sshCmd := rsyncSSHCommand(ssh)
			rsyncArgs = append(rsyncArgs, "-e", sshCmd)

			if direction == "push" {
				rsyncArgs = append(rsyncArgs, source, sshTarget+":"+dest)
			} else {
				rsyncArgs = append(rsyncArgs, sshTarget+":"+source, dest)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			execCmd := exec.CommandContext(ctx, rsyncBinary, rsyncArgs...)
			output, err := execCmd.CombinedOutput()
			if err != nil {
				if ctx.Err() != nil {
					return writeError(cmd, "timeout", "rsync timed out", "")
				}
				return writeError(cmd, "sync_failed", string(output), "")
			}

			writeAudit("transfer_sync", "", ssh.Host, ssh.User, "rsync "+direction, 0, countLines(output), 0)

			return writeJSON(cmd, map[string]any{
				"ok":        true,
				"host":      ssh.Host,
				"user":      ssh.User,
				"direction": direction,
				"source":    source,
				"dest":      dest,
				"delete":    delete,
			})
		},
	}

	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&direction, "direction", "push", "sync direction: push or pull")
	cmd.Flags().StringVar(&source, "source", "", "source path")
	cmd.Flags().StringVar(&dest, "dest", "", "destination path")
	cmd.Flags().BoolVar(&delete, "delete", false, "delete extraneous files at destination")
	cmd.Flags().StringArrayVar(&excludes, "exclude", nil, "exclude pattern (repeatable)")
	return cmd
}

func rsyncSSHCommand(ssh sshOptions) string {
	args := []string{"ssh", "-T"}
	if ssh.Port != 0 && ssh.Port != 22 {
		args = append(args, "-p", fmt.Sprintf("%d", ssh.Port))
	}
	if ssh.Identity != "" {
		args = append(args, "-i", ssh.Identity)
	}
	if ssh.Jump != "" {
		args = append(args, "-J", ssh.Jump)
	}
	if value := strictHostKeyCheckingRSYNC(ssh.HostKeyPolicy); value != "" {
		args = append(args, "-o", "StrictHostKeyChecking="+value)
	}
	return shellJoin(args)
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuoteIfNeeded(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuoteIfNeeded(arg string) string {
	if arg == "" || strings.ContainsAny(arg, " \t\n'\"\\;$&|<>`()[]{}!*?") {
		return remote.SingleQuote(arg)
	}
	return arg
}

func strictHostKeyCheckingRSYNC(policy string) string {
	switch policy {
	case "accept-new":
		return "accept-new"
	case "strict":
		return "yes"
	default:
		return ""
	}
}
