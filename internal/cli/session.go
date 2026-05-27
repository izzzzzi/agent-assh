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
	"github.com/izzzzzi/agent-assh/internal/safety"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/state"
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
		newSessionReadCommand(),
		newSessionCloseCommand(),
		newSessionListCommand(),
		newSessionExportCommand(),
		newSessionGCCommand(),
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
			if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("session_open", sid, ssh.Host, ssh.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, response.OK{
				"ok":           true,
				"install_tmux": installTmux,
				"session":      label,
				"sid":          sid,
				"tmux_name":    metadata.TmuxName,
				"host":         ssh.Host,
				"user":         ssh.User,
			})
		},
	}

	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVarP(&label, "name", "n", "", "session label")
	cmd.Flags().BoolVar(&installTmux, "install-tmux", false, "install tmux if missing")
	cmd.Flags().DurationVar(&ttl, "ttl", 12*time.Hour, "session ttl")
	return cmd
}

func newSessionExecCommand() *cobra.Command {
	var sid string
	var timeout int
	var confirmDanger bool
	ssh := defaultSSHOptions()

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
			if timeout < 1 {
				return writeInvalidArgs(cmd, "timeout must be greater than 0", "")
			}
			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}
			userCommand := remoteCommand(args)
			if result := safety.CheckCommand(userCommand); result.Dangerous && !confirmDanger {
				return writeError(cmd, "dangerous_command_requires_confirmation", "command looks destructive; rerun with --confirm-danger if intentional", result.Message)
			}
			entry.Seq++
			remoteCommand, err := session.ExecRemoteCommand(entry.SID, entry.TmuxName, entry.Seq, userCommand, timeout)
			if err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			localTimeout := time.Duration(timeout+5) * time.Second
			ctx, cancel := context.WithTimeout(cmd.Context(), localTimeout)
			defer cancel()
			if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, firstNonEmpty(ssh.Jump, entry.Jump), timeout+5, entry.HostKeyPolicy), remoteCommand)
			if strings.Contains(string(result.Stdout), "__ASSH_TIMEOUT__") {
				return writeError(cmd, "timeout", "session command timed out", "")
			}
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			rc, stdoutLines, stderrLines, timedOut := parseSessionExec(result.Stdout)
			if timedOut {
				return writeError(cmd, "timeout", "session command timed out", "")
			}
			writeAudit("session_exec", entry.SID, entry.Host, entry.User, remoteCommand, rc, stdoutLines, stderrLines)

			return writeJSON(cmd, response.OK{
				"ok":           true,
				"rc":           rc,
				"seq":          entry.Seq,
				"stdout_lines": stdoutLines,
				"stderr_lines": stderrLines,
				"sid":          sid,
				"session":      entry.Label,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 300, "timeout in seconds")
	cmd.Flags().BoolVar(&confirmDanger, "confirm-danger", false, "allow a command that matches destructive safety rules")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func newSessionReadCommand() *cobra.Command {
	var sid string
	var seq int
	var stream string
	var offset int
	var limit int
	var raw bool
	ssh := defaultSSHOptions()

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
			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}
			remoteCommand, err := session.ReadRemoteCommand(entry.SID, seq, stream, offset, limit)
			if err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 300*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, firstNonEmpty(ssh.Jump, entry.Jump), 300, entry.HostKeyPolicy), remoteCommand)
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			content, total, notFound := parseSessionRead(result.Stdout)
			if notFound {
				return writeError(cmd, "output_not_found", "session output not found", "")
			}
			if err := state.NewSessionOutputStore(stateBaseDir()).Write(state.SessionOutputPage{
				SID:        sid,
				Seq:        seq,
				Stream:     stream,
				Offset:     offset,
				Limit:      limit,
				TotalLines: total,
				Content:    content,
			}); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			if raw {
				_, err := cmd.OutOrStdout().Write([]byte(content))
				return err
			}
			hasMore := offset+limit < total
			writeAudit("session_read", entry.SID, entry.Host, entry.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, response.OK{
				"ok":          true,
				"sid":         sid,
				"seq":         seq,
				"stream":      stream,
				"offset":      offset,
				"limit":       limit,
				"total_lines": total,
				"has_more":    hasMore,
				"content":     content,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().IntVar(&seq, "seq", 0, "session command sequence")
	cmd.Flags().StringVar(&stream, "stream", "stdout", "stdout|stderr")
	cmd.Flags().IntVar(&offset, "offset", 0, "line offset")
	cmd.Flags().IntVar(&limit, "limit", 50, "line limit")
	cmd.Flags().BoolVar(&raw, "raw", false, "print only content without JSON")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
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
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, firstNonEmpty(ssh.Jump, entry.Jump), 300, entry.HostKeyPolicy), remoteCommand)
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if err := session.DeleteRegistry(stateBaseDir(), sid); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("session_close", entry.SID, entry.Host, entry.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
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
				result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, entry.Jump, 300, entry.HostKeyPolicy), remoteCommand)
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
				writeAudit("session_gc", entry.SID, entry.Host, entry.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
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

func sessionSSH(host, user string, port int, identity string, jump string, timeout int, policy string) transport.SSHCommand {
	return transport.SSHCommand{
		Host:          host,
		User:          user,
		Port:          port,
		Identity:      identity,
		Jump:          jump,
		TimeoutSecond: timeout,
		HostKeyPolicy: policy,
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
	return "if command -v apt >/dev/null 2>&1; then sudo -n apt update >/dev/null 2>&1 && sudo -n apt install -y tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v dnf >/dev/null 2>&1; then sudo -n dnf install -y tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v yum >/dev/null 2>&1; then sudo -n yum install -y tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v apk >/dev/null 2>&1; then sudo -n apk add tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v pacman >/dev/null 2>&1; then sudo -n pacman -Sy --noconfirm tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"elif command -v brew >/dev/null 2>&1; then brew install tmux || { echo tmux_install_failed >&2; exit 1; }; " +
		"else echo tmux_missing >&2; exit 127; fi"
}

func parseSessionRead(stdout []byte) (string, int, bool) {
	text := string(stdout)
	if strings.TrimSpace(text) == "__ASSH_NOT_FOUND__" {
		return "", 0, true
	}
	marker := "\n__ASSH_TOTAL_LINES__="
	idx := strings.LastIndex(text, marker)
	if idx == -1 {
		return text, countLines(stdout), false
	}
	totalText := strings.TrimSpace(text[idx+len(marker):])
	total, err := strconv.Atoi(totalText)
	if err != nil {
		total = countLines([]byte(text[:idx]))
	}
	return text[:idx], total, false
}

func parseSessionExec(stdout []byte) (int, int, int, bool) {
	text := string(stdout)
	if strings.Contains(text, "__ASSH_TIMEOUT__") {
		return -1, 0, 0, true
	}
	rc := markerInt(text, "__ASSH_RC__", 0)
	stdoutLines := markerInt(text, "__ASSH_STDOUT_LINES__", 0)
	stderrLines := markerInt(text, "__ASSH_STDERR_LINES__", 0)
	return rc, stdoutLines, stderrLines, false
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
