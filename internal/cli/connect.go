package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/bootstrap"
	"github.com/izzzzzi/agent-assh/internal/ids"
	"github.com/izzzzzi/agent-assh/internal/serverinfo"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

func newConnectCommand() *cobra.Command {
	var req bootstrap.Request
	ssh := defaultSSHOptions()
	ssh.Identity = filepath.Join(homeDir(), ".ssh", "id_agent_ed25519")

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
			ssh.applyToBootstrapRequest(&req)
			req.Timeout = time.Duration(ssh.TimeoutSecond) * time.Second
			return runConnect(cmd, req)
		},
	}

	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVarP(&req.PasswordEnv, "password-env", "E", "", "password environment variable for first login")
	cmd.Flags().StringVarP(&req.SessionName, "name", "n", "", "session label")
	cmd.Flags().DurationVar(&req.TTL, "ttl", 12*time.Hour, "session ttl")
	cmd.Flags().DurationVar(&req.GCOlderThan, "gc-older-than", 24*time.Hour, "cleanup sessions older than duration")
	cmd.Flags().BoolVar(&req.SkipGC, "no-gc", false, "skip bootstrap cleanup")
	cmd.Flags().BoolVar(&req.SkipTmuxInstall, "no-install-tmux", false, "do not install tmux if missing")
	return cmd
}

func newConnectInfoCommand() *cobra.Command {
	var req bootstrap.Request
	ssh := defaultSSHOptions()
	ssh.Identity = filepath.Join(homeDir(), ".ssh", "id_agent_ed25519")
	var file string

	cmd := &cobra.Command{
		Use:           "connect-info",
		Short:         "Parse pasted server info and open an agent tmux session",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			input, err := readServerInfoInput(cmd, file)
			if err != nil {
				return writeError(cmd, "invalid_args", err.Error(), "")
			}
			info, err := serverinfo.Parse(string(input))
			if err != nil {
				return writeError(cmd, "invalid_args", err.Error(), "")
			}

			req.Host = info.Host
			req.User = info.User
			if info.Port != 0 && !cmd.Flags().Changed("port") {
				ssh.Port = info.Port
			}
			ssh.Host = req.Host
			ssh.User = req.User
			ssh.applyToBootstrapRequest(&req)
			req.PasswordEnv = "__ASSH_CONNECT_INFO_PASSWORD"
			req.Timeout = time.Duration(ssh.TimeoutSecond) * time.Second
			service := newBootstrapService()
			service.LookupEnv = func(name string) (string, bool) {
				if name == req.PasswordEnv {
					return info.Password, true
				}
				return os.LookupEnv(name)
			}
			return runConnectWithService(cmd, req, service)
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "server info file; reads stdin when omitted")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{port: true, identity: true, jump: true, timeout: true, hostKeyPolicy: true})
	cmd.Flags().StringVarP(&req.SessionName, "name", "n", "", "session label")
	cmd.Flags().DurationVar(&req.TTL, "ttl", 12*time.Hour, "session ttl")
	cmd.Flags().DurationVar(&req.GCOlderThan, "gc-older-than", 24*time.Hour, "cleanup sessions older than duration")
	cmd.Flags().BoolVar(&req.SkipGC, "no-gc", false, "skip bootstrap cleanup")
	cmd.Flags().BoolVar(&req.SkipTmuxInstall, "no-install-tmux", false, "do not install tmux if missing")
	return cmd
}

func readServerInfoInput(cmd *cobra.Command, file string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	return io.ReadAll(cmd.InOrStdin())
}

func runConnect(cmd *cobra.Command, req bootstrap.Request) error {
	return runConnectWithService(cmd, req, newBootstrapService())
}

func runConnectWithService(cmd *cobra.Command, req bootstrap.Request, service bootstrap.Service) error {
	req.StateDir = stateBaseDir()
	if req.Identity == "" {
		req.Identity = filepath.Join(homeDir(), ".ssh", "id_agent_ed25519")
	}

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

	writeAudit("connect", "", req.Host, req.User, "connect", 0, 0, 0)
	return writeJSON(cmd, result)
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
		Jump:          target.Jump,
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
		Jump:          target.Jump,
		TimeoutSecond: target.TimeoutSecond,
		HostKeyPolicy: target.HostKeyPolicy,
	}
	return runSSHWithPassword(ctx, password, ssh, keyDeployRemoteCommand(strings.TrimSpace(string(pubKey))))
}
