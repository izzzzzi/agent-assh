package cli

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/ids"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

func newSessionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "session",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "session subcommand required", "run assh session --help")
		},
	}

	cmd.AddCommand(
		newSessionOpenCommand(),
		newSessionExecCommand(),
		newSessionExecAsyncCommand(),
		newSessionJobStatusCommand(),
		newSessionJobCancelCommand(),
		newSessionReadCommand(),
		newSessionCloseCommand(),
		newSessionListCommand(),
		newSessionExportCommand(),
		newSessionGCCommand(),
		newSessionProcessListCommand(),
		newSessionProcessKillCommand(),
		newSessionServiceCommand(),
		newSessionWatchCommand(),
		newSessionDBQueryCommand(),
		newSessionDockerPSCommand(),
		newSessionDockerLogsCommand(),
		newSessionDockerExecCommand(),
	)
	return cmd
}

func newSessionOpenCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var label string
	var installTmux bool
	var ttl time.Duration

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
			return runSessionOpen(cmd, ssh, label, installTmux, ttl)
		},
	}

	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVarP(&label, "name", "n", "", "session label")
	cmd.Flags().BoolVar(&installTmux, "install-tmux", false, "install tmux if missing")
	cmd.Flags().DurationVar(&ttl, "ttl", 12*time.Hour, "session ttl")
	return cmd
}

func runSessionOpen(cmd *cobra.Command, ssh sshOptions, label string, installTmux bool, ttl time.Duration) error {
	if err := ssh.validate(true); err != nil {
		return writeInvalidArgs(cmd, err.Error(), "")
	}
	if ttl <= 0 {
		return writeInvalidArgs(cmd, "ttl must be greater than 0", "")
	}

	sid, err := ids.New()
	if err != nil {
		return writeError(cmd, "internal_error", err.Error(), "")
	}
	metadata := session.NewMetadata(sid, label, ttl, "")
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return writeError(cmd, "internal_error", err.Error(), "")
	}
	remoteCommand, err := session.OpenRemoteCommand(string(metaJSON), metadata.TmuxName)
	if err != nil {
		return writeInvalidArgs(cmd, err.Error(), "")
	}
	if installTmux {
		remoteCommand = "command -v tmux >/dev/null 2>&1 || " + installTmuxCommand() + "; " + remoteCommand
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
	defer cancel()
	result := runSSH(ctx, ssh.command(), remoteCommand)
	if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
		return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
	}

	if err := saveSessionOpenRegistry(sid, label, ssh, metadata); err != nil {
		return writeError(cmd, "internal_error", err.Error(), "")
	}
	writeAudit("session_open", sid, ssh.Host, ssh.User, remoteCommand, result.ExitCode,
		countLines(result.Stdout), countLines(result.Stderr))

	return writeJSON(cmd, response.OK{
		"ok":           true,
		"install_tmux": installTmux,
		"session":      label,
		"sid":          sid,
		"tmux_name":    metadata.TmuxName,
		"host":         ssh.Host,
		"user":         ssh.User,
	})
}

// saveSessionOpenRegistry persists the session entry derived from ssh options
// and the freshly created metadata.
func saveSessionOpenRegistry(sid, label string, ssh sshOptions, metadata session.Metadata) error {
	entry := session.RegistryEntry{
		SID:           sid,
		Label:         label,
		Host:          ssh.Host,
		User:          ssh.User,
		Port:          ssh.Port,
		Identity:      ssh.Identity,
		Jump:          ssh.Jump,
		HostKeyPolicy: ssh.HostKeyPolicy,
		TmuxName:      metadata.TmuxName,
		CreatedAt:     metadata.CreatedAt,
		TTLSeconds:    metadata.TTLSeconds,
	}
	return session.SaveRegistry(stateBaseDir(), entry)
}

func newSessionCloseCommand() *cobra.Command {
	var sid string
	ssh := defaultSSHOptions()
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
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}
			remoteCommand, err := session.CloseRemoteCommand(entry.SID, entry.TmuxName)
			if err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 300*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity,
				firstNonEmpty(ssh.Jump, entry.Jump), 300, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if err := session.DeleteRegistry(stateBaseDir(), sid); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("session_close", entry.SID, entry.Host, entry.User, remoteCommand,
				result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
			return writeJSON(cmd, response.OK{"ok": true, "sid": sid, "session": entry.Label})
		},
	}
	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func newSessionGCCommand() *cobra.Command {
	var execute bool
	var host string
	var olderThan time.Duration
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
			entries, err := session.ListRegistry(stateBaseDir())
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			candidates := make([]string, 0)
			deleted := make([]string, 0)
			cleanupErrors := make([]map[string]string, 0)
			now := time.Now().UTC()
			for _, entry := range entries {
				if host != "" && entry.Host != host {
					continue
				}
				if olderThan > 0 && entry.CreatedAt.After(now.Add(-olderThan)) {
					continue
				}
				if olderThan == 0 && !(session.Metadata{CreatedAt: entry.CreatedAt, TTLSeconds: entry.TTLSeconds}).Expired(now) {
					continue
				}

				candidates = append(candidates, entry.SID)
				if !execute {
					continue
				}

				remoteCommand, err := session.GCRemoteCommand(entry.SID, entry.TmuxName)
				if err != nil {
					cleanupErrors = append(cleanupErrors, map[string]string{"sid": entry.SID, "error": err.Error()})
					continue
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), 300*time.Second)
				result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity,
					entry.Jump, 300, entry.HostKeyPolicy, false), remoteCommand)
				code := lifecycleResultErrorCode(ctx.Err(), result)
				cancel()
				if code != "" {
					cleanupErrors = append(cleanupErrors, map[string]string{"sid": entry.SID, "error": code})
					continue
				}
				if err := session.DeleteRegistry(stateBaseDir(), entry.SID); err != nil {
					cleanupErrors = append(cleanupErrors, map[string]string{"sid": entry.SID, "error": err.Error()})
					continue
				}
				deleted = append(deleted, entry.SID)
				writeAudit("session_gc", entry.SID, entry.Host, entry.User, remoteCommand,
					result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
			}
			return writeJSON(cmd, response.OK{
				"ok":         true,
				"dry_run":    !execute,
				"candidates": candidates,
				"deleted":    deleted,
				"errors":     cleanupErrors,
			})
		},
	}
	cmd.Flags().BoolVar(&execute, "execute", false, "delete remote sessions and local registry entries")
	cmd.Flags().StringVar(&host, "host", "", "filter by host")
	cmd.Flags().DurationVar(&olderThan, "older-than", 0, "include sessions older than duration")
	return cmd
}

func sessionSSH(host, user string, port int, identity string, jump string, timeout int, policy string,
	forcePTY bool) transport.SSHCommand {
	return transport.SSHCommand{
		Host:          host,
		User:          user,
		Port:          port,
		Identity:      identity,
		Jump:          jump,
		TimeoutSecond: timeout,
		HostKeyPolicy: policy,
		ForcePTY:      forcePTY,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func installTmuxCommand() string {
	return "if command -v apt >/dev/null 2>&1; then " +
		"sudo -n apt update >/dev/null 2>&1 && sudo -n apt install -y tmux || " +
		"{ echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v dnf >/dev/null 2>&1; then " +
		"sudo -n dnf install -y tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v yum >/dev/null 2>&1; then " +
		"sudo -n yum install -y tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v apk >/dev/null 2>&1; then " +
		"sudo -n apk add tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v pacman >/dev/null 2>&1; then " +
		"sudo -n pacman -Sy --noconfirm tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v brew >/dev/null 2>&1; then " +
		"brew install tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"else echo tmux_missing >&2; exit 127; fi"
}

func markerInt(text string, marker string, fallback int) int {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, marker+"=") {
			value, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, marker+"=")))
			if err == nil {
				return value
			}
		}
	}
	return fallback
}
