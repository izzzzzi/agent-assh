package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-ssh/assh/internal/bootstrap"
	"github.com/agent-ssh/assh/internal/ids"
	"github.com/agent-ssh/assh/internal/transport"
	"github.com/spf13/cobra"
)

func newConnectCommand() *cobra.Command {
	var req bootstrap.Request
	var timeoutSeconds int

	cmd := &cobra.Command{
		Use:           "connect",
		Short:         "Bootstrap SSH access and open an agent tmux session",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: "  export TARGET_PASS='...'\n" +
			"  assh connect -H 10.0.0.1 -u root -E TARGET_PASS -n deploy\n" +
			"  unset TARGET_PASS\n\n" +
			"  assh connect -H 10.0.0.1 -u root -i ~/.ssh/id_agent_ed25519 -n deploy",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Timeout = time.Duration(timeoutSeconds) * time.Second
			req.StateDir = stateBaseDir()
			if req.Identity == "" {
				req.Identity = filepath.Join(homeDir(), ".ssh", "id_agent_ed25519")
			}

			service := newBootstrapService()

			ctx, cancel := context.WithTimeout(cmd.Context(), req.Timeout)
			defer cancel()

			result, err := service.Run(ctx, req)
			if err != nil {
				var bootErr bootstrap.Error
				if errors.As(err, &bootErr) {
					return writeError(cmd, bootErr.Code, bootErr.Message, bootErr.Hint)
				}
				return writeError(cmd, "internal_error", err.Error(), "")
			}

			writeAudit("connect", req.Host, req.User, "connect", 0, 0, 0)
			return writeJSON(cmd, result)
		},
	}

	cmd.Flags().StringVarP(&req.Host, "host", "H", "", "SSH host")
	cmd.Flags().StringVarP(&req.User, "user", "u", "root", "SSH user")
	cmd.Flags().IntVarP(&req.Port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&req.Identity, "identity", "i", filepath.Join(homeDir(), ".ssh", "id_agent_ed25519"), "identity file")
	cmd.Flags().StringVarP(&req.PasswordEnv, "password-env", "E", "", "password environment variable for first login")
	cmd.Flags().StringVarP(&req.SessionName, "name", "n", "", "session label")
	cmd.Flags().DurationVar(&req.TTL, "ttl", 12*time.Hour, "session ttl")
	cmd.Flags().IntVarP(&timeoutSeconds, "timeout", "t", 300, "timeout in seconds")
	cmd.Flags().StringVar(&req.HostKeyPolicy, "host-key-policy", "accept-new", "host key policy: accept-new, strict, no-check")
	cmd.Flags().DurationVar(&req.GCOlderThan, "gc-older-than", 24*time.Hour, "cleanup sessions older than duration")
	cmd.Flags().BoolVar(&req.SkipGC, "no-gc", false, "skip bootstrap cleanup")
	cmd.Flags().BoolVar(&req.SkipTmuxInstall, "no-install-tmux", false, "do not install tmux if missing")
	return cmd
}

var newBootstrapService = func() bootstrap.Service {
	return bootstrap.Service{
		RunSSH:         runBootstrapSSH,
		EnsureKeyPair:  ensureKeyPair,
		DeployPassword: deployPublicKeyWithPassword,
		LookupEnv:      os.LookupEnv,
		NewID:          ids.New,
	}
}

func runBootstrapSSH(ctx context.Context, target bootstrap.SSHTarget, remoteCommand string) bootstrap.SSHResult {
	result := runSSH(ctx, transport.SSHCommand{
		Host:          target.Host,
		User:          target.User,
		Port:          target.Port,
		Identity:      target.Identity,
		TimeoutSecond: target.TimeoutSecond,
		HostKeyPolicy: target.HostKeyPolicy,
	}, remoteCommand)
	return bootstrap.SSHResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
		Err:      result.Err,
	}
}

func deployPublicKeyWithPassword(ctx context.Context, password string, target bootstrap.SSHTarget, identity string) error {
	pubKey, err := os.ReadFile(identity + ".pub")
	if err != nil {
		return err
	}
	ssh := transport.SSHCommand{
		Host:          target.Host,
		User:          target.User,
		Port:          target.Port,
		TimeoutSecond: target.TimeoutSecond,
		HostKeyPolicy: target.HostKeyPolicy,
	}
	return runSSHWithPassword(ctx, password, ssh.Args(keyDeployRemoteCommand(strings.TrimSpace(string(pubKey)))))
}
