package cli

import (
	"github.com/izzzzzi/agent-assh/internal/bootstrap"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

type sshOptions struct {
	Host          string
	User          string
	Port          int
	Identity      string
	Jump          string
	TimeoutSecond int
	HostKeyPolicy string
}

func defaultSSHOptions() sshOptions {
	return sshOptions{
		User:          "root",
		Port:          22,
		TimeoutSecond: 300,
		HostKeyPolicy: "accept-new",
	}
}

func bindSSHOptions(cmd *cobra.Command, opts *sshOptions, cfg sshOptionFlags) {
	if cfg.host {
		cmd.Flags().StringVarP(&opts.Host, "host", "H", opts.Host, "SSH host")
	}
	if cfg.user {
		cmd.Flags().StringVarP(&opts.User, "user", "u", opts.User, "SSH user")
	}
	if cfg.port {
		cmd.Flags().IntVarP(&opts.Port, "port", "p", opts.Port, "SSH port")
	}
	if cfg.identity {
		cmd.Flags().StringVarP(&opts.Identity, "identity", "i", opts.Identity, "SSH identity file")
	}
	if cfg.jump {
		cmd.Flags().StringVarP(&opts.Jump, "jump", "J", opts.Jump, "SSH jump host")
	}
	if cfg.timeout {
		cmd.Flags().IntVarP(&opts.TimeoutSecond, "timeout", "t", opts.TimeoutSecond, "timeout in seconds")
	}
	if cfg.hostKeyPolicy {
		cmd.Flags().StringVar(&opts.HostKeyPolicy, "host-key-policy", opts.HostKeyPolicy, "host key policy: accept-new, strict, no-check")
	}
}

type sshOptionFlags struct {
	host          bool
	user          bool
	port          bool
	identity      bool
	jump          bool
	timeout       bool
	hostKeyPolicy bool
}

func standardSSHOptionFlags() sshOptionFlags {
	return sshOptionFlags{
		host:          true,
		user:          true,
		port:          true,
		identity:      true,
		jump:          true,
		timeout:       true,
		hostKeyPolicy: true,
	}
}

func (o sshOptions) validate(requireHost bool) error {
	if requireHost && o.Host == "" {
		return validationError("host required")
	}
	if o.Port < 1 || o.Port > 65535 {
		return validationError("port must be between 1 and 65535")
	}
	if o.TimeoutSecond < 1 {
		return validationError("timeout must be greater than 0")
	}
	if !validHostKeyPolicy(o.HostKeyPolicy) {
		return validationError("invalid host key policy")
	}
	return nil
}

func (o sshOptions) command() transport.SSHCommand {
	return transport.SSHCommand{
		Host:          o.Host,
		User:          o.User,
		Port:          o.Port,
		Identity:      o.Identity,
		Jump:          o.Jump,
		TimeoutSecond: o.TimeoutSecond,
		HostKeyPolicy: o.HostKeyPolicy,
	}
}

func (o sshOptions) applyToBootstrapRequest(req *bootstrap.Request) {
	req.Host = o.Host
	req.User = o.User
	req.Port = o.Port
	req.Identity = o.Identity
	req.Jump = o.Jump
	req.HostKeyPolicy = o.HostKeyPolicy
}

type validationError string

func (e validationError) Error() string { return string(e) }
