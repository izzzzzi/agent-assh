package cli

import (
	"context"
	"time"

	"github.com/izzzzzi/agent-assh/internal/capabilities"
	"github.com/spf13/cobra"
)

func newCapabilitiesCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	ssh.TimeoutSecond = 30

	cmd := &cobra.Command{
		Use:           "capabilities",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			result := runSSH(ctx, ssh.command(), capabilities.ProbeCommand())

			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			writeAudit("capabilities", ssh.Host, ssh.User, capabilities.ProbeCommand(), result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, capabilities.ParseProbe(result.Stdout))
		},
	}

	bindSSHOptions(cmd, &ssh, sshOptionFlags{host: true, user: true, port: true, identity: true, jump: true})
	return cmd
}
