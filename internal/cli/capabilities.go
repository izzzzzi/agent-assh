package cli

import (
	"context"
	"strings"
	"time"

	"github.com/agent-ssh/assh/internal/capabilities"
	"github.com/agent-ssh/assh/internal/transport"
	"github.com/spf13/cobra"
)

func newCapabilitiesCommand() *cobra.Command {
	var host string
	var user string
	var port int
	var identity string

	cmd := &cobra.Command{
		Use:           "capabilities",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				return writeInvalidArgs(cmd, "host required", "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			result := transport.SSHCommand{
				Host:          host,
				User:          user,
				Port:          port,
				Identity:      identity,
				TimeoutSecond: 30,
				HostKeyPolicy: "accept-new",
			}.Run(ctx, capabilities.ProbeCommand())

			if result.Err != nil {
				return writeError(cmd, "connection_error", connectionErrorMessage(result), "")
			}

			return writeJSON(cmd, capabilities.ParseProbe(result.Stdout))
		},
	}

	cmd.Flags().StringVarP(&host, "host", "H", "", "SSH host")
	cmd.Flags().StringVarP(&user, "user", "u", "root", "SSH user")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&identity, "identity", "i", "", "SSH identity file")
	return cmd
}

func connectionErrorMessage(result transport.Result) string {
	stderr := strings.TrimSpace(string(result.Stderr))
	if stderr != "" {
		return stderr
	}
	return result.Err.Error()
}
