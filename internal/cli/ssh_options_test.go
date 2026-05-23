package cli

import (
	"testing"

	"github.com/izzzzzi/agent-assh/internal/bootstrap"
)

func TestSSHOptionsBuildCommandIncludesJump(t *testing.T) {
	opts := sshOptions{
		Host:          "example.com",
		User:          "root",
		Port:          2222,
		Identity:      "key",
		Jump:          "bastion.example.com",
		TimeoutSecond: 30,
		HostKeyPolicy: "strict",
	}

	cmd := opts.command()

	if cmd.Host != opts.Host || cmd.User != opts.User || cmd.Port != opts.Port || cmd.Identity != opts.Identity || cmd.Jump != opts.Jump {
		t.Fatalf("command=%#v opts=%#v", cmd, opts)
	}
}

func TestSSHOptionsApplyToBootstrapRequestIncludesJump(t *testing.T) {
	opts := sshOptions{
		Host:          "example.com",
		User:          "root",
		Port:          2222,
		Identity:      "key",
		Jump:          "bastion.example.com",
		HostKeyPolicy: "accept-new",
	}
	var req bootstrap.Request
	opts.applyToBootstrapRequest(&req)

	if req.Jump != opts.Jump {
		t.Fatalf("req.Jump=%q want %q", req.Jump, opts.Jump)
	}
}
