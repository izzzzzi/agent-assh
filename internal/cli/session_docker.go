package cli

import (
	"context"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/spf13/cobra"
)

func newSessionDockerPSCommand() *cobra.Command {
	var sid string
	var all bool
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "docker-ps",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			flags := ""
			if all {
				flags = "-a"
			}
			remoteCommand := "docker ps " + flags + " --format '{{.ID}}\\t{{.Names}}\\t{{.Image}}\\t{{.Status}}\\t{{.Ports}}' 2>&1"
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, 30, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			return writeJSON(cmd, map[string]any{
				"ok":     true,
				"sid":    sid,
				"all":    all,
				"output": string(result.Stdout),
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "show all containers (including stopped)")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func newSessionDockerLogsCommand() *cobra.Command {
	var sid string
	var container string
	var tail int
	var follow bool
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "docker-logs",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if container == "" {
				return writeInvalidArgs(cmd, "--container is required", "")
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			if tail < 1 {
				tail = 50
			}
			flags := "--tail " + itoaStr(tail)
			if follow {
				flags += " -f"
				flags += " --since 5s"
			}

			remoteCommand := "docker logs " + flags + " " + remote.SingleQuote(container) + " 2>&1"
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, ssh.TimeoutSecond, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			return writeJSON(cmd, map[string]any{
				"ok":        true,
				"sid":       sid,
				"container": container,
				"tail":      tail,
				"output":    string(result.Stdout),
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVarP(&container, "container", "c", "", "container name or id")
	cmd.Flags().IntVarP(&tail, "tail", "n", 50, "number of lines to tail")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output (short snapshot)")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func newSessionDockerExecCommand() *cobra.Command {
	var sid string
	var container string
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "docker-exec",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if container == "" {
				return writeInvalidArgs(cmd, "--container is required", "")
			}
			if len(args) == 0 {
				return writeInvalidArgs(cmd, "command required", "")
			}

			if result, handled, errReturn := classifyCommand(cmd, remoteCommand(args)); handled {
				return errReturn
			} else if result.Dangerous {
				return writeError(cmd, "dangerous_command_requires_confirmation", "docker-exec command looks destructive", result.Message)
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			remoteCommand := "docker exec " + remote.SingleQuote(container) + " " + remoteCommand(args) + " 2>&1"
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, ssh.TimeoutSecond, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			return writeJSON(cmd, map[string]any{
				"ok":        true,
				"sid":       sid,
				"container": container,
				"output":    string(result.Stdout),
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVarP(&container, "container", "c", "", "container name or id")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}
