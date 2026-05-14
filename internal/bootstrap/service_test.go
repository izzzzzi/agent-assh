package bootstrap

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-ssh/assh/internal/session"
)

var errFakeSSH = errors.New("ssh failed")

func validRequest(t *testing.T) Request {
	return Request{
		Host:          "10.0.0.1",
		User:          "root",
		Port:          22,
		Identity:      t.TempDir() + "/id_agent_ed25519",
		SessionName:   "deploy",
		TTL:           12 * time.Hour,
		Timeout:       time.Minute,
		HostKeyPolicy: "accept-new",
		GCOlderThan:   24 * time.Hour,
		StateDir:      t.TempDir(),
	}
}

func TestRunValidatesRequiredFields(t *testing.T) {
	service := Service{}
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{name: "host", req: Request{User: "root", Port: 22, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "port low", req: Request{Host: "example.com", User: "root", Port: 0, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "ttl", req: Request{Host: "example.com", User: "root", Port: 22, Identity: "/tmp/id", TTL: 0, Timeout: time.Second, HostKeyPolicy: "accept-new", StateDir: t.TempDir()}, want: "invalid_args"},
		{name: "policy", req: Request{Host: "example.com", User: "root", Port: 22, Identity: "/tmp/id", TTL: time.Hour, Timeout: time.Second, HostKeyPolicy: "bad", StateDir: t.TempDir()}, want: "invalid_args"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Run(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			bootErr, ok := err.(Error)
			if !ok {
				t.Fatalf("expected bootstrap.Error, got %T", err)
			}
			if bootErr.Code != tt.want {
				t.Fatalf("code=%q want %q", bootErr.Code, tt.want)
			}
		})
	}
}

func TestRunDoesNotReadPasswordWhenKeyLoginWorks(t *testing.T) {
	req := validRequest(t)
	req.PasswordEnv = "TARGET_PASS"
	deployCalled := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			if command == keyCheckCommand {
				return SSHResult{ExitCode: 0}
			}
			return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
		},
		DeployPassword: func(context.Context, string, SSHTarget, string) error {
			deployCalled = true
			return nil
		},
		LookupEnv: func(string) (string, bool) {
			t.Fatal("password env was read even though key login worked")
			return "", false
		},
		NewID: func() (string, error) { return "abc12345", nil },
	}
	_, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if deployCalled {
		t.Fatal("password deployer was called even though key login worked")
	}
}

func TestRunReturnsAuthFailedWhenKeyLoginFailsWithoutPasswordEnv(t *testing.T) {
	req := validRequest(t)
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		RunSSH: func(context.Context, SSHTarget, string) SSHResult {
			return SSHResult{ExitCode: 255, Err: errFakeSSH, Stderr: []byte("Permission denied")}
		},
		NewID: func() (string, error) { return "abc12345", nil },
	}
	_, err := service.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected auth_failed")
	}
	bootErr := err.(Error)
	if bootErr.Code != "auth_failed" {
		t.Fatalf("code=%q want auth_failed", bootErr.Code)
	}
}

func TestSSHErrorCodeClassifiesMissingLocalSSH(t *testing.T) {
	result := SSHResult{
		ExitCode: -1,
		Err:      &exec.Error{Name: "ssh", Err: exec.ErrNotFound},
	}
	if got := sshErrorCode(nil, result); got != "ssh_missing" {
		t.Fatalf("sshErrorCode() = %q want ssh_missing", got)
	}
}

func TestRunDeploysAndVerifiesKeyWhenPasswordEnvIsProvided(t *testing.T) {
	req := validRequest(t)
	req.PasswordEnv = "TARGET_PASS"
	sshCalls := 0
	keyCheckCalls := 0
	deployCalls := 0
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			sshCalls++
			if command == keyCheckCommand {
				keyCheckCalls++
			}
			if command == keyCheckCommand && keyCheckCalls == 1 {
				return SSHResult{ExitCode: 255, Err: errFakeSSH, Stderr: []byte("Permission denied")}
			}
			return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
		},
		DeployPassword: func(_ context.Context, password string, _ SSHTarget, _ string) error {
			deployCalls++
			if password != "secret" {
				t.Fatalf("password=%q want secret", password)
			}
			return nil
		},
		LookupEnv: func(name string) (string, bool) {
			if name == "TARGET_PASS" {
				return "secret", true
			}
			return "", false
		},
		NewID: func() (string, error) { return "abc12345", nil },
	}
	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if deployCalls != 1 {
		t.Fatalf("deployCalls=%d want 1", deployCalls)
	}
	if keyCheckCalls != 2 {
		t.Fatalf("keyCheckCalls=%d want 2 (sshCalls=%d)", keyCheckCalls, sshCalls)
	}
	if !result.KeyDeployed || !result.KeyVerified {
		t.Fatalf("key flags = deployed:%v verified:%v", result.KeyDeployed, result.KeyVerified)
	}
}

func TestRunReturnsKeyDeployFailedWhenPostDeployVerificationFails(t *testing.T) {
	req := validRequest(t)
	req.PasswordEnv = "TARGET_PASS"
	sshCalls := 0
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			sshCalls++
			if command != keyCheckCommand {
				t.Fatalf("command=%q want %q", command, keyCheckCommand)
			}
			return SSHResult{ExitCode: 255, Err: errFakeSSH, Stderr: []byte("Permission denied")}
		},
		DeployPassword: func(context.Context, string, SSHTarget, string) error {
			return nil
		},
		LookupEnv: func(name string) (string, bool) {
			if name == "TARGET_PASS" {
				return "secret", true
			}
			return "", false
		},
		NewID: func() (string, error) { return "abc12345", nil },
	}

	_, err := service.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected key_deploy_failed")
	}
	bootErr := err.(Error)
	if bootErr.Code != "key_deploy_failed" {
		t.Fatalf("code=%q want key_deploy_failed", bootErr.Code)
	}
	if sshCalls != 2 {
		t.Fatalf("sshCalls=%d want 2", sshCalls)
	}
}

func TestRunInstallsTmuxWhenProbeReportsMissing(t *testing.T) {
	req := validRequest(t)
	commands := []string{}
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			commands = append(commands, command)
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=missing\npkg=apt\ninstall=noninteractive\n")}
			case command == installTmuxRemoteCommand:
				return SSHResult{ExitCode: 0}
			case strings.Contains(command, "tmux new-session") && strings.Contains(command, "assh_abc12345"):
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.TmuxInstalled {
		t.Fatal("TmuxInstalled=false want true")
	}
	if result.SID != "abc12345" || result.Session != "deploy" || result.TmuxName != "assh_abc12345" {
		t.Fatalf("session fields = sid:%q session:%q tmux:%q", result.SID, result.Session, result.TmuxName)
	}
	wantNext := map[string]string{
		"exec":  `assh session exec -s abc12345 -- "pwd"`,
		"read":  "assh session read -s abc12345 --seq 1 --limit 50",
		"close": "assh session close -s abc12345",
	}
	for key, want := range wantNext {
		if result.NextCommands[key] != want {
			t.Fatalf("NextCommands[%q]=%q want %q", key, result.NextCommands[key], want)
		}
	}
	if len(commands) != 4 {
		t.Fatalf("commands=%d want 4: %#v", len(commands), commands)
	}
	wantKinds := []string{"key-check", "probe", "install-tmux", "open-session"}
	if got := commandKinds(commands); !slices.Equal(got, wantKinds) {
		t.Fatalf("command kinds=%v want %v", got, wantKinds)
	}

	entry, err := session.LoadRegistry(req.StateDir, "abc12345")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if entry.SID != "abc12345" || entry.Label != "deploy" || entry.TmuxName != "assh_abc12345" {
		t.Fatalf("registry entry = sid:%q label:%q tmux:%q", entry.SID, entry.Label, entry.TmuxName)
	}
}

func commandKinds(commands []string) []string {
	kinds := make([]string, 0, len(commands))
	for _, command := range commands {
		switch {
		case command == keyCheckCommand:
			kinds = append(kinds, "key-check")
		case command == probeCommand:
			kinds = append(kinds, "probe")
		case command == installTmuxRemoteCommand:
			kinds = append(kinds, "install-tmux")
		case strings.Contains(command, "tmux new-session"):
			kinds = append(kinds, "open-session")
		default:
			kinds = append(kinds, "unknown")
		}
	}
	return kinds
}

func TestRunOpensSessionWhenTmuxAlreadyInstalled(t *testing.T) {
	req := validRequest(t)
	installed := false
	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case command == installTmuxRemoteCommand:
				installed = true
				return SSHResult{ExitCode: 0}
			case strings.Contains(command, "tmux new-session") && strings.Contains(command, "assh_abc12345"):
				opened = true
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if installed {
		t.Fatal("install command was called")
	}
	if !opened {
		t.Fatal("open command was not called")
	}
	if !result.TmuxInstalled {
		t.Fatal("TmuxInstalled=false want true")
	}
}

func TestRunReturnsTmuxMissingWhenInstallDisabled(t *testing.T) {
	req := validRequest(t)
	req.SkipTmuxInstall = true
	installed := false
	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=missing\npkg=apt\ninstall=noninteractive\n")}
			case command == installTmuxRemoteCommand:
				installed = true
				return SSHResult{ExitCode: 0}
			case strings.Contains(command, "tmux new-session"):
				opened = true
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	_, err := service.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected tmux_missing")
	}
	bootErr := err.(Error)
	if bootErr.Code != "tmux_missing" {
		t.Fatalf("code=%q want tmux_missing", bootErr.Code)
	}
	if installed {
		t.Fatal("install command was called")
	}
	if opened {
		t.Fatal("open command was called")
	}
}
