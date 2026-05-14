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

	"github.com/agent-ssh/assh/internal/audit"
	"github.com/agent-ssh/assh/internal/ids"
	"github.com/agent-ssh/assh/internal/response"
	"github.com/agent-ssh/assh/internal/state"
	"github.com/agent-ssh/assh/internal/transport"
	"github.com/spf13/cobra"
)

func newExecCommand() *cobra.Command {
	var host string
	var user string
	var port int
	var identity string
	var timeout int
	var hostKeyPolicy string

	cmd := &cobra.Command{
		Use:           "exec -- command",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				return writeInvalidArgs(cmd, "host required", "")
			}
			if len(args) == 0 {
				return writeInvalidArgs(cmd, "command required", "")
			}
			if timeout < 1 {
				return writeInvalidArgs(cmd, "timeout must be greater than 0", "")
			}
			if port < 1 || port > 65535 {
				return writeInvalidArgs(cmd, "port must be between 1 and 65535", "")
			}
			if !validHostKeyPolicy(hostKeyPolicy) {
				return writeInvalidArgs(cmd, "invalid host key policy", "")
			}

			outputID, err := ids.New()
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(timeout)*time.Second)
			defer cancel()

			result := runSSH(ctx, transport.SSHCommand{
				Host:          host,
				User:          user,
				Port:          port,
				Identity:      identity,
				TimeoutSecond: timeout,
				HostKeyPolicy: hostKeyPolicy,
			}, remoteCommand(args))

			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			store := state.NewOutputStore(filepath.Join(stateBaseDir(), "outputs"))
			if err := store.Write(outputID, result.Stdout, result.Stderr); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("exec", host, user, remoteCommand(args), result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, response.OK{
				"ok":           true,
				"exit_code":    result.ExitCode,
				"output_id":    outputID,
				"stdout_lines": countLines(result.Stdout),
				"stderr_lines": countLines(result.Stderr),
			})
		},
	}

	cmd.Flags().StringVarP(&host, "host", "H", "", "SSH host")
	cmd.Flags().StringVarP(&user, "user", "u", "root", "SSH user")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&identity, "identity", "i", "", "SSH identity file")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 300, "timeout in seconds")
	cmd.Flags().StringVar(&hostKeyPolicy, "host-key-policy", "accept-new", "host key policy")
	return cmd
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

func sshErrorCode(ctxErr, runErr error) string {
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

func sshResultErrorCode(ctxErr error, result transport.Result) string {
	if code := sshErrorCode(ctxErr, result.Err); code != "" {
		return code
	}
	if result.Err == nil || result.ExitCode != 255 {
		return ""
	}
	stderr := strings.ToLower(string(result.Stderr))
	switch {
	case strings.Contains(stderr, "permission denied"), strings.Contains(stderr, "authentication failed"):
		return "auth_failed"
	case strings.Contains(stderr, "host key verification failed"), strings.Contains(stderr, "remote host identification has changed"):
		return "host_key_failed"
	case strings.Contains(stderr, "connection refused"),
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
		return ""
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

func writeAudit(action, host, user, command string, exitCode int, stdoutLines int, stderrLines int) {
	hash := ""
	if command != "" {
		sum := sha256.Sum256([]byte(command))
		hash = fmt.Sprintf("%x", sum[:])
	}
	_ = audit.Write(filepath.Join(stateBaseDir(), "audit", "audit.jsonl"), audit.Event{
		Action:      action,
		Host:        host,
		User:        user,
		CommandHash: hash,
		ExitCode:    exitCode,
		StdoutLines: stdoutLines,
		StderrLines: stderrLines,
	})
}

var runSSH = func(ctx context.Context, command transport.SSHCommand, remoteCommand string) transport.Result {
	return command.Run(ctx, remoteCommand)
}

func stateBaseDir() string {
	return state.BaseDir()
}
