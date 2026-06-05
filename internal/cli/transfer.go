package cli

import (
	"context"
	"os"
	"time"

	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

func newTransferCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "transfer",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "transfer subcommand required", "run assh transfer --help")
		},
	}
	cmd.AddCommand(
		newTransferPutCommand(),
		newTransferGetCommand(),
		newTransferListCommand(),
		newTransferStatCommand(),
		newTransferMkdirCommand(),
		newTransferRmCommand(),
		newTransferMvCommand(),
		newTransferSyncCommand(),
	)
	return cmd
}

func newTransferPutCommand() *cobra.Command {
	return newTransferLeafCommand("put", transport.Upload)
}

func newTransferGetCommand() *cobra.Command {
	return newTransferLeafCommand("get", transport.Download)
}

func newTransferLeafCommand(use string, direction transport.SCPDirection) *cobra.Command {
	ssh := defaultSSHOptions()
	cmd := &cobra.Command{
		Use:           use + " SOURCE DEST",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return writeInvalidArgs(cmd, "source and destination required", "")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}

			source := args[0]
			destination := args[1]
			bytes, err := transferSizeBefore(direction, source)
			if err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			result := runSCP(ctx, scpCommandFromSSHOptions(ssh), source, destination, direction)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.Err != nil || result.ExitCode != 0 {
				return writeError(cmd, "transfer_failed", sshResultErrorMessage(ctx.Err(), result), "")
			}

			if direction == transport.Download {
				bytes, err = localFileSize(destination, "local destination")
				if err != nil {
					return writeError(cmd, "transfer_failed", err.Error(), "")
				}
			}
			writeAudit("transfer_"+use, "", ssh.Host, ssh.User, "transfer "+use, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, response.OK{
				"ok":          true,
				"host":        ssh.Host,
				"user":        ssh.User,
				"source":      source,
				"destination": destination,
				"bytes":       bytes,
			})
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	return cmd
}

func transferSizeBefore(direction transport.SCPDirection, source string) (int64, error) {
	if direction != transport.Upload {
		return 0, nil
	}
	return localFileSize(source, "local source")
}

func localFileSize(path string, label string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, validationError(label + " is not readable")
	}
	if info.IsDir() {
		return 0, validationError(label + " must be a file")
	}
	return info.Size(), nil
}

func scpCommandFromSSHOptions(ssh sshOptions) transport.SCPCommand {
	return transport.SCPCommand{
		Host:          ssh.Host,
		User:          ssh.User,
		Port:          ssh.Port,
		Identity:      ssh.Identity,
		Jump:          ssh.Jump,
		TimeoutSecond: ssh.TimeoutSecond,
		HostKeyPolicy: ssh.HostKeyPolicy,
	}
}

var runSCP = func(ctx context.Context, command transport.SCPCommand, source string, destination string, direction transport.SCPDirection) transport.Result {
	return command.Run(ctx, source, destination, direction)
}
