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
			return runConnect(cmd, req)
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

func newConnectInfoCommand() *cobra.Command {
	var req bootstrap.Request
	var timeoutSeconds int
	var file string

	cmd := &cobra.Command{
		Use:           "connect-info",
		Short:         "Parse pasted server info and open an agent tmux session",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
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
			req.PasswordEnv = "__ASSH_CONNECT_INFO_PASSWORD"
			req.Timeout = time.Duration(timeoutSeconds) * time.Second
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
	cmd.Flags().IntVarP(&req.Port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&req.Identity, "identity", "i", filepath.Join(homeDir(), ".ssh", "id_agent_ed25519"), "identity file")
	cmd.Flags().StringVarP(&req.SessionName, "name", "n", "", "session label")
	cmd.Flags().DurationVar(&req.TTL, "ttl", 12*time.Hour, "session ttl")
	cmd.Flags().IntVarP(&timeoutSeconds, "timeout", "t", 300, "timeout in seconds")
	cmd.Flags().StringVar(&req.HostKeyPolicy, "host-key-policy", "accept-new", "host key policy: accept-new, strict, no-check")
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

	writeAudit("connect", req.Host, req.User, "connect", 0, 0, 0)
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
