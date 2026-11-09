package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/audit"
	"github.com/izzzzzi/agent-assh/internal/ids"
	"github.com/izzzzzi/agent-assh/internal/redact"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

func newExecCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var noRedact bool

	cmd := &cobra.Command{
		Use:           "exec -- command",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return writeInvalidArgs(cmd, "command required", "")
			}
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}

			outputID, err := ids.New()
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}

			if result, handled, errReturn := classifyCommand(cmd, remoteCommand(args)); handled {
				return errReturn
			} else if result.Dangerous {
				return writeError(cmd, "dangerous_command_requires_confirmation",
					"command looks destructive; assh exec does not support --confirm-danger",
					destructiveCommandHint(ssh, remoteCommand(args), result.Message))
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			result := runSSH(ctx, ssh.command(), remoteCommand(args))

			if code := remoteCommandResultErrorCode(ctx.Err(), result); code != "" {
				hint := ""
				if code == "timeout" {
					hint = "do not blindly retry; use `assh session exec-async -s SID -- \"cmd\"` " +
						"for long commands, then `assh session job-status`"
				}
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), hint)
			}

			stdout, stderr := result.Stdout, result.Stderr
			redactionCount := 0
			if !noRedact {
				var outRes, errRes redact.Result
				stdout, outRes = redact.Bytes(stdout)
				stderr, errRes = redact.Bytes(stderr)
				redactionCount = outRes.Count + errRes.Count
			}

			store := state.NewOutputStore(filepath.Join(stateBaseDir(), "outputs"))
			if err := store.Write(outputID, stdout, stderr); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			stdoutLines := countLines(stdout)
			stderrLines := countLines(stderr)
			writeAudit("exec", "", ssh.Host, ssh.User, remoteCommand(args), result.ExitCode, stdoutLines, stderrLines)

			return writeJSON(cmd, response.OK{
				"ok":              true,
				"exit_code":       result.ExitCode,
				"output_id":       outputID,
				"stdout_lines":    stdoutLines,
				"stderr_lines":    stderrLines,
				"redacted":        redactionCount > 0,
				"redaction_count": redactionCount,
			})
		},
	}

	cmd.Flags().BoolVar(&noRedact, "no-redact", false, "disable best-effort secret redaction in stored output")
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	return cmd
}

func destructiveCommandHint(ssh sshOptions, command string, reason string) string {
	entries, err := session.ListRegistry(stateBaseDir())
	if err == nil {
		for _, entry := range entries {
			if entry.Host != ssh.Host || entry.User != ssh.User || entry.Port != ssh.Port {
				continue
			}
			if entry.Identity != "" && ssh.Identity != "" && entry.Identity != ssh.Identity {
				continue
			}
			return "open registered session " + entry.SID + " and rerun: " +
				"assh session exec -s " + entry.SID + " --confirm-danger -- " +
				remote.SingleQuote(command) + "; if intentional: " + reason
		}
	}
	return "open a session and rerun with `assh session exec -s SID --confirm-danger -- " +
		remote.SingleQuote(command) + "` if intentional: " + reason
}

func newReadCommand() *cobra.Command {
	var outputID string
	var stream string
	var offset int
	var limit int
	var raw bool

	cmd := &cobra.Command{
		Use:           "read",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if outputID == "" {
				return writeInvalidArgs(cmd, "id required", "")
			}
			if !ids.Valid(outputID) {
				return writeInvalidArgs(cmd, "invalid output id", "")
			}
			if stream != "stdout" && stream != "stderr" {
				return writeInvalidArgs(cmd, "stream must be stdout or stderr", "")
			}
			if offset < 0 {
				return writeInvalidArgs(cmd, "offset must be non-negative", "")
			}
			if limit < 1 {
				return writeInvalidArgs(cmd, "limit must be at least 1", "")
			}

			store := state.NewOutputStore(filepath.Join(stateBaseDir(), "outputs"))
			page, err := store.Read(outputID, stream, offset, limit)
			if err != nil {
				return writeError(cmd, "output_not_found", err.Error(), "")
			}
			servedLines := countLines([]byte(page.Content))
			writeAuditSavings("read", "", page.TotalLines, servedLines)
			if raw {
				_, _ = cmd.OutOrStdout().Write([]byte(page.Content))
				return nil
			}
			return writeJSON(cmd, page)
		},
	}

	cmd.Flags().StringVar(&outputID, "id", "", "output id")
	cmd.Flags().StringVar(&stream, "stream", "stdout", "output stream")
	cmd.Flags().IntVar(&offset, "offset", 0, "line offset")
	cmd.Flags().IntVar(&limit, "limit", 50, "line limit")
	cmd.Flags().BoolVar(&raw, "raw", false, "print only content without JSON")
	return cmd
}

func remoteCommand(args []string) string {
	return strings.Join(args, " ")
}

func validHostKeyPolicy(policy string) bool {
	return policy == "accept-new" || policy == "strict" || policy == "no-check"
}

// localSSHErrorCode classifies a failure of the local ssh process itself, where
// no remote exit status is available. Named with a "local" prefix to keep it
// distinct from bootstrap.sshErrorCode, which classifies a remote SSHResult and
// lives in a sibling package.
func localSSHErrorCode(ctxErr, runErr error) string {
	if ctxErr != nil {
		return "timeout"
	}
	if runErr == nil {
		return ""
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return ""
	}

	var execErr *exec.Error
	if errors.As(runErr, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return "ssh_missing"
	}

	return "connection_error"
}

// sshResultErrorCode classifies a transport failure for session lifecycle calls.
// A remote exit status of 255 is treated as a connection error there.
func sshResultErrorCode(ctxErr error, result transport.Result) string {
	return classifySSHResult(ctxErr, result, false)
}

// remoteCommandResultErrorCode classifies a transport failure for one-off
// commands (`exec`, `transfer`). Unlike the session path, a bare 255 with no
// local error is the remote command's own exit status and must be passed through
// so callers can report it verbatim.
func remoteCommandResultErrorCode(ctxErr error, result transport.Result) string {
	return classifySSHResult(ctxErr, result, true)
}

// classifySSHResult maps a transport result onto a typed error code. Exit code
// 255 is ambiguous: ssh itself uses it for connection-level failures, but a
// remote command may legitimately exit 255 too. allowRemoteExit255 selects which
// reading applies, and stderr is inspected first to catch ssh's own diagnostics.
func classifySSHResult(ctxErr error, result transport.Result, allowRemoteExit255 bool) string {
	if ctxErr != nil {
		return "timeout"
	}
	if result.ExitCode == 255 {
		stderr := strings.ToLower(string(result.Stderr))
		switch {
		case strings.Contains(stderr, "permission denied"), strings.Contains(stderr, "authentication failed"):
			return "auth_failed"
		case strings.Contains(stderr, "scp: permission denied"):
			return "auth_failed"
		case strings.Contains(stderr, "host key verification failed"),
			strings.Contains(stderr, "remote host identification has changed"):
			return "host_key_failed"
		case strings.Contains(stderr, "too many authentication failures"):
			return "auth_failed"
		case strings.Contains(stderr, "hostname contains invalid characters"),
			strings.Contains(stderr, "bad configuration option"),
			strings.Contains(stderr, "connection refused"),
			strings.Contains(stderr, "could not resolve hostname"),
			strings.Contains(stderr, "no route to host"),
			strings.Contains(stderr, "connection timed out"),
			strings.Contains(stderr, "operation timed out"),
			strings.Contains(stderr, "connection closed"),
			strings.Contains(stderr, "kex_exchange_identification"),
			strings.Contains(stderr, "banner exchange"),
			strings.Contains(stderr, "network is unreachable"):
			return "connection_error"
		default:
			if allowRemoteExit255 {
				var exitErr *exec.ExitError
				if result.Err == nil || errors.As(result.Err, &exitErr) {
					return ""
				}
			}
			return "connection_error"
		}
	}
	if code := localSSHErrorCode(nil, result.Err); code != "" {
		return code
	}
	return ""
}

func lifecycleResultErrorCode(ctxErr error, result transport.Result) string {
	if code := sshResultErrorCode(ctxErr, result); code != "" {
		return code
	}
	if result.Err == nil && result.ExitCode == 0 {
		return ""
	}
	stderr := strings.ToLower(strings.TrimSpace(string(result.Stderr)))
	switch {
	case strings.Contains(stderr, "tmux_missing"):
		return "tmux_missing"
	case strings.Contains(stderr, "tmux_install_failed"):
		return "tmux_install_failed"
	default:
		return "command_failed"
	}
}

func sshErrorMessage(ctxErr, runErr error) string {
	if ctxErr != nil {
		return ctxErr.Error()
	}
	if runErr != nil {
		return runErr.Error()
	}
	return ""
}

func sshResultErrorMessage(ctxErr error, result transport.Result) string {
	if ctxErr != nil {
		return ctxErr.Error()
	}
	stderr := strings.TrimSpace(string(result.Stderr))
	if stderr != "" {
		return stderr
	}
	return sshErrorMessage(ctxErr, result.Err)
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lines := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}

func writeAudit(action, sid, host, user, command string, exitCode int, stdoutLines int, stderrLines int) {
	hash := ""
	if command != "" {
		sum := sha256.Sum256([]byte(command))
		hash = fmt.Sprintf("%x", sum[:])
	}
	_ = audit.Write(filepath.Join(stateBaseDir(), "audit", "audit.jsonl"), audit.Event{
		Action:      action,
		SID:         sid,
		Host:        host,
		User:        user,
		CommandHash: hash,
		ExitCode:    exitCode,
		StdoutLines: stdoutLines,
		StderrLines: stderrLines,
	})
}

// writeAuditSavings records a read action with raw vs served line counts for the
// `audit --savings` aggregate. action is typically "read" or "session_read".
func writeAuditSavings(action, sid string, rawLines, servedLines int) {
	_ = audit.Write(filepath.Join(stateBaseDir(), "audit", "audit.jsonl"), audit.Event{
		Action:      action,
		SID:         sid,
		RawLines:    rawLines,
		ServedLines: servedLines,
	})
}

var runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
	return command.Run(ctx, remoteCommand)
}

func stateBaseDir() string {
	return state.BaseDir()
}
