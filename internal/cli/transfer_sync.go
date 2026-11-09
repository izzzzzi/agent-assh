package cli

import (
	"context"
	"errors"
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
	var deleteFlag bool
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
			// No --info=progress2: rsync < 3.1.0 (still common on remote hosts)
			// rejects it with "unrecognized option", and the progress stream is
			// noise in captured output anyway.
			rsyncArgs := []string{"-az"}

			if deleteFlag {
				rsyncArgs = append(rsyncArgs, "--delete")
			}
			for _, ex := range excludes {
				rsyncArgs = append(rsyncArgs, "--exclude="+ex)
			}

			// SSH options for rsync
			sshTarget := ssh.User + "@" + ssh.Host
			sshCmd := rsyncSSHCommand(ssh)
			rsyncArgs = append(rsyncArgs, "-e", sshCmd)

			operands, err := rsyncOperands(direction, source, dest, sshTarget)
			if err != nil {
				return writeInvalidArgs(cmd, err.Error(),
					"rename the path, or use `assh transfer put`/`get`, which do not go through a remote shell")
			}
			rsyncArgs = append(rsyncArgs, operands...)

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			execCmd := exec.CommandContext(ctx, rsyncBinary, rsyncArgs...)
			output, err := execCmd.CombinedOutput()
			if err != nil {
				if ctx.Err() != nil {
					return writeError(cmd, "timeout", "rsync timed out", "")
				}
				return writeError(cmd, "sync_failed", firstNonEmpty(strings.TrimSpace(string(output)), err.Error()), "")
			}

			writeAudit("transfer_sync", "", ssh.Host, ssh.User, "rsync "+direction, 0, countLines(output), 0)

			return writeJSON(cmd, map[string]any{
				"ok":        true,
				"host":      ssh.Host,
				"user":      ssh.User,
				"direction": direction,
				"source":    source,
				"dest":      dest,
				"delete":    deleteFlag,
			})
		},
	}

	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&direction, "direction", "push", "sync direction: push or pull")
	cmd.Flags().StringVar(&source, "source", "", "source path")
	cmd.Flags().StringVar(&dest, "dest", "", "destination path")
	cmd.Flags().BoolVar(&deleteFlag, "delete", false, "delete extraneous files at destination")
	cmd.Flags().StringArrayVar(&excludes, "exclude", nil, "exclude pattern (repeatable)")
	return cmd
}

// rsyncOperands returns the `-- SRC DST` tail of the rsync command line.
func rsyncOperands(direction, source, dest, sshTarget string) ([]string, error) {
	remotePath := dest
	if direction == "pull" {
		remotePath = source
	}
	// rsync hands the remote path to the remote shell, and whether it escapes
	// it depends on the rsync version on both ends.
	if strings.ContainsAny(remotePath, "$`;|&<>()'\"\\\n\r") {
		return nil, errors.New("remote path contains shell metacharacters")
	}
	// `--` ends option parsing: a local path starting with `-` must not be
	// read as an rsync option (e.g. --rsh=...).
	if direction == "push" {
		return []string{"--", source, sshTarget + ":" + dest}, nil
	}
	return []string{"--", sshTarget + ":" + source, dest}, nil
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
	if ssh.Batch {
		args = append(args, "-o", "BatchMode=yes")
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
	case "no-check":
		return "no"
	default:
		return ""
	}
}
