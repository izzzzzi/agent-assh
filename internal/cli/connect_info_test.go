package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/bootstrap"
)

func TestRootIncludesConnectInfoCommand(t *testing.T) {
	cmd := NewRootCommand()
	found, _, err := cmd.Find([]string{"connect-info"})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if found == nil || found.Name() != "connect-info" {
		t.Fatalf("Find(connect-info) = %v, want connect-info command", found)
	}
}

func TestConnectInfoParsesFileAndUsesInMemoryPassword(t *testing.T) {
	infoFile := t.TempDir() + "/server.txt"
	if err := os.WriteFile(infoFile, []byte(`IPv4-адрес сервера: 203.0.113.10 copy icon
Пользователь: root copy icon
Пароль: example\npassword$1 copy icon`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	original := newBootstrapService
	t.Cleanup(func() { newBootstrapService = original })
	deployPassword := ""
	newBootstrapService = func() bootstrap.Service {
		return bootstrap.Service{
			EnsureKeyPair: func(string) error { return nil },
			RunSSH: func(_ context.Context, _ bootstrap.SSHTarget, _ string) bootstrap.SSHResult {
				return bootstrap.SSHResult{
					Stderr:   []byte("Permission denied"),
					ExitCode: 255,
					Err:      errors.New("ssh failed"),
				}
			},
			DeployPassword: func(_ context.Context, password string, target bootstrap.SSHTarget, _ string) error {
				deployPassword = password
				if target.Host != "203.0.113.10" || target.User != "root" {
					t.Fatalf("target=%#v", target)
				}
				return errors.New("stop after capture")
			},
			NewID: func() (string, error) { return "abc12345", nil },
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"connect-info", "--file", infoFile, "-n", "deploy"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected key deploy error")
	}
	if deployPassword != "example\npassword$1" {
		t.Fatalf("deployPassword=%q", deployPassword)
	}
	if strings.Contains(stdout.String()+stderr.String(), "example") || strings.Contains(stdout.String()+stderr.String(), "password") {
		t.Fatalf("command output leaked password: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	var got map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("expected json stderr, got %q", stderr.String())
	}
	if got["error"] != "key_deploy_failed" {
		t.Fatalf("unexpected response: %#v", got)
	}
}
