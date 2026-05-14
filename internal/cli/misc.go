package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-ssh/assh/internal/remote"
	"github.com/agent-ssh/assh/internal/transport"
	"github.com/spf13/cobra"
)

func newScanCommand() *cobra.Command {
	var host, user, identity string
	var port int
	cmd := &cobra.Command{
		Use:           "scan",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				return writeInvalidArgs(cmd, "host required", "")
			}
			if port < 1 || port > 65535 {
				return writeInvalidArgs(cmd, "port must be between 1 and 65535", "")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, transport.SSHCommand{Host: host, User: user, Port: port, Identity: identity, TimeoutSecond: 30, HostKeyPolicy: "accept-new"}, scanRemoteCommand())
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			writeAudit("scan", host, user, scanRemoteCommand(), result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
			_, _ = cmd.OutOrStdout().Write(result.Stdout)
			if len(result.Stdout) == 0 || result.Stdout[len(result.Stdout)-1] != '\n' {
				_, _ = cmd.OutOrStdout().Write([]byte("\n"))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&host, "host", "H", "", "SSH host")
	cmd.Flags().StringVarP(&user, "user", "u", "root", "SSH user")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&identity, "identity", "i", "", "SSH identity file")
	return cmd
}

func newKeyDeployCommand() *cobra.Command {
	var host, user, envName, identity string
	var port int
	cmd := &cobra.Command{
		Use:           "key-deploy",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				return writeInvalidArgs(cmd, "host required", "")
			}
			if envName == "" {
				return writeInvalidArgs(cmd, "--password-env required", "")
			}
			if os.Getenv(envName) == "" {
				return writeInvalidArgs(cmd, "password env is empty", "")
			}
			if port < 1 || port > 65535 {
				return writeInvalidArgs(cmd, "port must be between 1 and 65535", "")
			}
			if err := ensureKeyPair(identity); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			pubKey, err := os.ReadFile(identity + ".pub")
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			err = runSSHWithPassword(ctx, os.Getenv(envName), transport.SSHCommand{
				Host:          host,
				User:          user,
				Port:          port,
				TimeoutSecond: 60,
				HostKeyPolicy: "accept-new",
			}.Args(keyDeployRemoteCommand(strings.TrimSpace(string(pubKey)))))
			if err != nil {
				return writeError(cmd, "connection_error", err.Error(), "")
			}
			writeAudit("key_deploy", host, user, "key-deploy", 0, 0, 0)
			return writeJSON(cmd, map[string]any{
				"ok":       true,
				"host":     host,
				"user":     user,
				"identity": identity,
			})
		},
	}
	cmd.Flags().StringVarP(&host, "host", "H", "", "SSH host")
	cmd.Flags().StringVarP(&user, "user", "u", "root", "SSH user")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&envName, "password-env", "E", "", "password environment variable")
	cmd.Flags().StringVarP(&identity, "identity", "i", filepath.Join(homeDir(), ".ssh", "id_agent_ed25519"), "identity file")
	return cmd
}

func newAuditCommand() *cobra.Command {
	var last int
	cmd := &cobra.Command{
		Use:           "audit",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if last < 1 {
				return writeInvalidArgs(cmd, "--last must be greater than 0", "")
			}
			body, err := os.ReadFile(filepath.Join(stateBaseDir(), "audit", "audit.jsonl"))
			if errors.Is(err, os.ErrNotExist) {
				return writeJSON(cmd, []string{})
			}
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			lines := strings.Split(strings.TrimSpace(string(body)), "\n")
			if len(lines) > last {
				lines = lines[len(lines)-last:]
			}
			_, _ = cmd.OutOrStdout().Write([]byte("["))
			for i, line := range lines {
				if i > 0 {
					_, _ = cmd.OutOrStdout().Write([]byte(","))
				}
				_, _ = cmd.OutOrStdout().Write([]byte(line))
			}
			_, _ = cmd.OutOrStdout().Write([]byte("]\n"))
			return nil
		},
	}
	cmd.Flags().IntVar(&last, "last", 20, "last audit entries")
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
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(identity), 0o700); err != nil {
		return err
	}
	return exec.Command("ssh-keygen", "-t", "ed25519", "-f", identity, "-N", "").Run()
}

func runSSHWithPassword(ctx context.Context, password string, args []string) error {
	dir, err := os.MkdirTemp("", "assh-askpass-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	askpass := filepath.Join(dir, "askpass.sh")
	if err := os.WriteFile(askpass, []byte("#!/bin/sh\nprintf '%s\\n' "+remote.SingleQuote(password)+"\n"), 0o700); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "ssh", args...)
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
	}
	command.Env = append(os.Environ(), "SSH_ASKPASS="+askpass, "SSH_ASKPASS_REQUIRE=force", "DISPLAY="+display)
	output, err := command.CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(output)))
	}
	return nil
}

func keyDeployRemoteCommand(pubKey string) string {
	quotedKey := remote.SingleQuote(pubKey)
	return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && " +
		"grep -qxF " + quotedKey + " ~/.ssh/authorized_keys 2>/dev/null || echo " + quotedKey + " >> ~/.ssh/authorized_keys; " +
		"chmod 600 ~/.ssh/authorized_keys"
}
