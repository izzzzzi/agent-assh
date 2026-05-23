package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/audit"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

func newScanCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	ssh.TimeoutSecond = 30
	cmd := &cobra.Command{
		Use:           "scan",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, ssh.command(), scanRemoteCommand())
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			writeAudit("scan", ssh.Host, ssh.User, scanRemoteCommand(), result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
			_, _ = cmd.OutOrStdout().Write(result.Stdout)
			if len(result.Stdout) == 0 || result.Stdout[len(result.Stdout)-1] != '\n' {
				_, _ = cmd.OutOrStdout().Write([]byte("\n"))
			}
			return nil
		},
	}
	bindSSHOptions(cmd, &ssh, sshOptionFlags{host: true, user: true, port: true, identity: true, jump: true})
	return cmd
}

func newKeyDeployCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	ssh.Identity = filepath.Join(homeDir(), ".ssh", "id_agent_ed25519")
	ssh.TimeoutSecond = 60
	var envName string
	cmd := &cobra.Command{
		Use:           "key-deploy",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if envName == "" {
				return writeInvalidArgs(cmd, "--password-env required", "")
			}
			if os.Getenv(envName) == "" {
				return writeInvalidArgs(cmd, "password env is empty", "")
			}
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if err := ensureKeyPair(ssh.Identity); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			pubKey, err := os.ReadFile(ssh.Identity + ".pub")
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			err = runSSHWithPassword(ctx, os.Getenv(envName), ssh.command(), keyDeployRemoteCommand(strings.TrimSpace(string(pubKey))))
			if err != nil {
				return writeError(cmd, passwordSSHErrorCode(err), err.Error(), "")
			}
			writeAudit("key_deploy", ssh.Host, ssh.User, "key-deploy", 0, 0, 0)
			return writeJSON(cmd, map[string]any{
				"ok":       true,
				"host":     ssh.Host,
				"user":     ssh.User,
				"identity": ssh.Identity,
			})
		},
	}
	bindSSHOptions(cmd, &ssh, sshOptionFlags{host: true, user: true, port: true, identity: true, jump: true})
	cmd.Flags().StringVarP(&envName, "password-env", "E", "", "password environment variable")
	return cmd
}

func newAuditCommand() *cobra.Command {
	var last int
	var host string
	var failed bool
	cmd := &cobra.Command{
		Use:           "audit",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if last < 1 {
				return writeInvalidArgs(cmd, "--last must be greater than 0", "")
			}
			events, err := audit.Read(filepath.Join(stateBaseDir(), "audit", "audit.jsonl"), audit.Filter{
				Last:   last,
				Host:   host,
				Failed: failed,
			})
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			return writeJSON(cmd, events)
		},
	}
	cmd.Flags().IntVar(&last, "last", 20, "last audit entries")
	cmd.Flags().StringVar(&host, "host", "", "filter by host")
	cmd.Flags().BoolVar(&failed, "failed", false, "show only failed events")
	return cmd
}

func scanRemoteCommand() string {
	return `printf '{"hostname":"%s","os":"%s","kernel":"%s","arch":"%s","cpu_cores":"%s","ip":"%s"}\n' "$(hostname 2>/dev/null)" "$(uname -s 2>/dev/null)" "$(uname -r 2>/dev/null)" "$(uname -m 2>/dev/null)" "$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo N/A)" "$(hostname -I 2>/dev/null | awk '{print $1}' || echo N/A)"`
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func ensureKeyPair(identity string) error {
	if _, err := os.Stat(identity); err == nil {
		if _, pubErr := os.Stat(identity + ".pub"); pubErr == nil {
			return nil
		}
		output, pubErr := exec.Command("ssh-keygen", "-y", "-f", identity).Output()
		if pubErr != nil {
			return pubErr
		}
		return os.WriteFile(identity+".pub", output, 0o600)
	}
	if err := os.MkdirAll(filepath.Dir(identity), 0o700); err != nil {
		return err
	}
	return exec.Command("ssh-keygen", "-t", "ed25519", "-f", identity, "-N", "").Run()
}

func runSSHWithPassword(ctx context.Context, password string, command transport.SSHCommand, remoteCommand string) error {
	dir, err := os.MkdirTemp("", "assh-askpass-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	askpass := filepath.Join(dir, "askpass.sh")
	if err := os.WriteFile(askpass, []byte("#!/bin/sh\nprintf '%s\\n' "+remote.SingleQuote(password)+"\n"), 0o500); err != nil {
		return err
	}
	execCommand := exec.CommandContext(ctx, "ssh", command.Args(remoteCommand)...)
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
	}
	execCommand.Env = sshAskpassEnv(askpass, display)
	output, err := execCommand.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return passwordSSHError{output: output, err: ctxErr}
		}
		return passwordSSHError{output: output, err: err}
	}
	return nil
}

func sshAskpassEnv(askpass string, display string) []string {
	keys := []string{"PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "LC_CTYPE", "TERM"}
	env := make([]string, 0, len(keys)+3)
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return append(env, "SSH_ASKPASS="+askpass, "SSH_ASKPASS_REQUIRE=force", "DISPLAY="+display)
}

type passwordSSHError struct {
	output []byte
	err    error
}

func (e passwordSSHError) Error() string {
	text := strings.TrimSpace(string(e.output))
	if text != "" {
		return text
	}
	if e.err != nil {
		return e.err.Error()
	}
	return "ssh command failed"
}

func passwordSSHErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var passwordErr passwordSSHError
	if errors.As(err, &passwordErr) {
		if errors.Is(passwordErr.err, context.DeadlineExceeded) || errors.Is(passwordErr.err, context.Canceled) {
			return "timeout"
		}
		var execErr *exec.Error
		if errors.As(passwordErr.err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return "ssh_missing"
		}
		var exitErr *exec.ExitError
		if errors.As(passwordErr.err, &exitErr) {
			if exitErr.ExitCode() == 255 {
				if code := passwordSSHTextErrorCode(passwordErr.Error()); code != "connection_error" {
					return code
				}
				return "connection_error"
			}
			return "command_failed"
		}
		if code := passwordSSHTextErrorCode(passwordErr.Error()); code != "connection_error" {
			return code
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return "ssh_missing"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := passwordSSHTextErrorCode(err.Error()); code != "connection_error" {
			return code
		}
		if exitErr.ExitCode() == 255 {
			return "connection_error"
		}
		return "command_failed"
	}
	return passwordSSHTextErrorCode(err.Error())
}

func passwordSSHTextErrorCode(text string) string {
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "permission denied"), strings.Contains(text, "authentication failed"):
		return "auth_failed"
	case strings.Contains(text, "host key verification failed"), strings.Contains(text, "remote host identification has changed"):
		return "host_key_failed"
	case strings.Contains(text, "tmux_missing"):
		return "tmux_missing"
	case strings.Contains(text, "tmux_install_failed"):
		return "tmux_install_failed"
	default:
		return "connection_error"
	}
}

func keyDeployRemoteCommand(pubKey string) string {
	quotedKey := remote.SingleQuote(pubKey)
	return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && " +
		"(grep -qxF " + quotedKey + " ~/.ssh/authorized_keys 2>/dev/null || echo " + quotedKey + " >> ~/.ssh/authorized_keys) && " +
		"chmod 600 ~/.ssh/authorized_keys"
}
