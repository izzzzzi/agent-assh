package cli

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/agent-ssh/assh/internal/ids"
	"github.com/agent-ssh/assh/internal/remote"
	"github.com/agent-ssh/assh/internal/response"
	"github.com/agent-ssh/assh/internal/session"
	"github.com/agent-ssh/assh/internal/transport"
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
	var user string
	var port int
	var identity string
	var label string
	var installTmux bool
	var timeout int
	var ttl time.Duration
	var hostKeyPolicy string

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
			if port < 1 || port > 65535 {
				return writeInvalidArgs(cmd, "port must be between 1 and 65535", "")
			}
			if timeout < 1 {
				return writeInvalidArgs(cmd, "timeout must be greater than 0", "")
			}
			if ttl <= 0 {
				return writeInvalidArgs(cmd, "ttl must be greater than 0", "")
			}
			if !validHostKeyPolicy(hostKeyPolicy) {
				return writeInvalidArgs(cmd, "invalid host key policy", "")
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

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(timeout)*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(host, user, port, identity, timeout, hostKeyPolicy), remoteCommand)
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			entry := session.RegistryEntry{
				SID:           sid,
				Label:         label,
				Host:          host,
				User:          user,
				Port:          port,
				Identity:      identity,
				HostKeyPolicy: hostKeyPolicy,
				TmuxName:      metadata.TmuxName,
				CreatedAt:     metadata.CreatedAt,
				TTLSeconds:    metadata.TTLSeconds,
			}
			if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("session_open", host, user, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, response.OK{
				"ok":           true,
				"install_tmux": installTmux,
				"session":      label,
				"sid":          sid,
				"host":         host,
				"user":         user,
			})
		},
	}

	cmd.Flags().StringVarP(&host, "host", "H", "", "SSH host")
	cmd.Flags().StringVarP(&user, "user", "u", "root", "SSH user")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&identity, "identity", "i", "", "SSH identity file")
	cmd.Flags().StringVarP(&label, "name", "n", "", "session label")
	cmd.Flags().BoolVar(&installTmux, "install-tmux", false, "install tmux if missing")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 300, "timeout in seconds")
	cmd.Flags().DurationVar(&ttl, "ttl", 12*time.Hour, "session ttl")
	cmd.Flags().StringVar(&hostKeyPolicy, "host-key-policy", "accept-new", "host key policy")
	return cmd
}

func newSessionExecCommand() *cobra.Command {
	var sid string
	var timeout int

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
			entry.Seq++
			remoteCommand, err := session.ExecRemoteCommand(entry.SID, entry.TmuxName, entry.Seq, remoteCommand(args), timeout)
			if err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			localTimeout := time.Duration(timeout+5) * time.Second
			ctx, cancel := context.WithTimeout(cmd.Context(), localTimeout)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, timeout+5, entry.HostKeyPolicy), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			rc, stdoutLines, stderrLines, timedOut := parseSessionExec(result.Stdout)
			if timedOut {
				return writeError(cmd, "timeout", "session command timed out", "")
			}
			if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("session_exec", entry.Host, entry.User, remoteCommand, rc, stdoutLines, stderrLines)

			return writeJSON(cmd, response.OK{
				"ok":           rc == 0,
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
	return cmd
}

func newSessionReadCommand() *cobra.Command {
	var sid string
	var seq int
	var stream string
	var offset int
	var limit int
	var raw bool

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
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, 300, entry.HostKeyPolicy), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			content, total, notFound := parseSessionRead(result.Stdout)
			if notFound {
				return writeError(cmd, "output_not_found", "session output not found", "")
			}
			if raw {
				_, err := cmd.OutOrStdout().Write([]byte(content))
				return err
			}
			hasMore := offset+limit < total
			writeAudit("session_read", entry.Host, entry.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

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
	return cmd
}

func newSessionCloseCommand() *cobra.Command {
	var sid string
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
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, 300, entry.HostKeyPolicy), remoteCommand)
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if err := session.DeleteRegistry(stateBaseDir(), sid); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("session_close", entry.Host, entry.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
			return writeJSON(cmd, response.OK{"ok": true, "sid": sid, "session": entry.Label})
		},
	}
	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	return cmd
}

func newSessionGCCommand() *cobra.Command {
	var execute bool
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
			now := time.Now().UTC()
			for _, entry := range entries {
				if (session.Metadata{CreatedAt: entry.CreatedAt, TTLSeconds: entry.TTLSeconds}).Expired(now) {
					candidates = append(candidates, entry.SID)
					if execute {
						_ = session.DeleteRegistry(stateBaseDir(), entry.SID)
					}
				}
			}
			return writeJSON(cmd, response.OK{
				"ok":         true,
				"dry_run":    !execute,
				"candidates": candidates,
			})
		},
	}
	cmd.Flags().BoolVar(&execute, "execute", false, "delete expired local registry entries")
	return cmd
}

func sessionSSH(host, user string, port int, identity string, timeout int, policy string) transport.SSHCommand {
	return transport.SSHCommand{
		Host:          host,
		User:          user,
		Port:          port,
		Identity:      identity,
		TimeoutSecond: timeout,
		HostKeyPolicy: policy,
	}
}

func installTmuxCommand() string {
	return "if command -v apt >/dev/null 2>&1; then sudo -n apt update >/dev/null 2>&1 && sudo -n apt install -y tmux; " +
		"elif command -v dnf >/dev/null 2>&1; then sudo -n dnf install -y tmux; " +
		"elif command -v yum >/dev/null 2>&1; then sudo -n yum install -y tmux; " +
		"elif command -v apk >/dev/null 2>&1; then sudo -n apk add tmux; " +
		"elif command -v pacman >/dev/null 2>&1; then sudo -n pacman -Sy --noconfirm tmux; " +
		"elif command -v brew >/dev/null 2>&1; then brew install tmux; " +
		"else echo tmux_missing >&2; exit 127; fi"
}

func parseSessionRead(stdout []byte) (string, int, bool) {
	text := string(stdout)
	if strings.Contains(text, "__ASSH_NOT_FOUND__") {
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
