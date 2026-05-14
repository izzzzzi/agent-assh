package bootstrap

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
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
	deployCalls := 0
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			sshCalls++
			if command == keyCheckCommand && sshCalls == 1 {
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
	if sshCalls != 2 {
		t.Fatalf("sshCalls=%d want 2", sshCalls)
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
