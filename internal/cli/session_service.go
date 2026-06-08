package cli

import (
	"context"
	"strconv"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/spf13/cobra"
)

func newSessionServiceCommand() *cobra.Command {
	var sid string
	var action string
	var service string
	var lines int
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "service",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if service == "" {
				return writeInvalidArgs(cmd, "--service is required", "")
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			var remoteCommand string
			switch action {
			case "status":
				remoteCommand = "systemctl status " + remote.SingleQuote(service) + " --no-pager -l 2>/dev/null || echo 'SERVICE_NOT_FOUND'"
			case "restart":
				remoteCommand = "sudo systemctl restart " + remote.SingleQuote(service) + " 2>&1 && echo 'RESTART_OK' || echo 'RESTART_FAILED'"
			case "start":
				remoteCommand = "sudo systemctl start " + remote.SingleQuote(service) + " 2>&1 && echo 'START_OK' || echo 'START_FAILED'"
			case "stop":
				remoteCommand = "sudo systemctl stop " + remote.SingleQuote(service) + " 2>&1 && echo 'STOP_OK' || echo 'STOP_FAILED'"
			case "logs":
				if lines < 1 {
					lines = 50
				}
				remoteCommand = "journalctl -u " + remote.SingleQuote(service) + " --no-pager -n " + strconv.Itoa(lines) + " 2>/dev/null || echo 'JOURNALCTL_UNAVAILABLE'"
			default:
				return writeInvalidArgs(cmd, "invalid action: "+action, "valid: status, restart, start, stop, logs")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, ssh.TimeoutSecond, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			return writeJSON(cmd, map[string]any{
				"ok":      true,
				"sid":     sid,
				"service": service,
				"action":  action,
				"output":  string(result.Stdout),
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVarP(&action, "action", "a", "status", "action: status, restart, start, stop, logs")
	cmd.Flags().StringVarP(&service, "service", "n", "", "service name (e.g., nginx, docker)")
	cmd.Flags().IntVarP(&lines, "lines", "l", 50, "journal lines (for logs action)")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}
