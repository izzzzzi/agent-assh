package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestReadMissingIDReturnsJSONError(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"read"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}

	var got map[string]any
	if json.Unmarshal(out.Bytes(), &got) != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "id required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestReadRawPrintsOnlyContent(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	store := state.NewOutputStore(filepath.Join(stateBaseDir(), "outputs"))
	if err := store.Write("abcdef12", []byte("one\ntwo\nthree\n"), []byte("err\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"read", "--id", "abcdef12", "--offset", "1", "--limit", "1", "--raw"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); got != "two\n" {
		t.Fatalf("raw output = %q, want %q", got, "two\n")
	}
}

func TestReadRawStderr(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	store := state.NewOutputStore(filepath.Join(stateBaseDir(), "outputs"))
	if err := store.Write("abcdef12", []byte("out\n"), []byte("err-a\nerr-b\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"read", "--id", "abcdef12", "--stream", "stderr", "--limit", "2", "--raw"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); got != "err-a\nerr-b\n" {
		t.Fatalf("raw stderr = %q", got)
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name string
		data string
		want int
	}{
		{name: "empty", data: "", want: 0},
		{name: "trailing newline", data: "a\nb\n", want: 2},
		{name: "non trailing final line", data: "a\nb", want: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := countLines([]byte(test.data)); got != test.want {
				t.Fatalf("countLines(%q) = %d, want %d", test.data, got, test.want)
			}
		})
	}
}

func TestExecRemoteCommandPreservesArgs(t *testing.T) {
	got := remoteCommand([]string{"ls", "-la", "/tmp"})
	if got != "ls -la /tmp" {
		t.Fatalf("remoteCommand() = %q, want %q", got, "ls -la /tmp")
	}
}

func TestReadInvalidArgsReturnsJSONError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "id", args: []string{"read", "--id", "../bad"}},
		{name: "stream", args: []string{"read", "--id", "abcdef12", "--stream", "bad"}},
		{name: "limit", args: []string{"read", "--id", "abcdef12", "--limit", "0"}},
		{name: "offset", args: []string{"read", "--id", "abcdef12", "--offset", "-1"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := executeJSONError(t, test.args)
			if got["error"] != "invalid_args" {
				t.Fatalf("unexpected response: %#v", got)
			}
		})
	}
}

func TestExecInvalidFlagsReturnJSONError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "timeout", args: []string{"exec", "--host", "example.com", "--timeout", "0", "--", "true"}},
		{name: "port low", args: []string{"exec", "--host", "example.com", "--port", "0", "--", "true"}},
		{name: "port high", args: []string{"exec", "--host", "example.com", "--port", "65536", "--", "true"}},
		{name: "policy", args: []string{"exec", "--host", "example.com", "--host-key-policy", "bad", "--", "true"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := executeJSONError(t, test.args)
			if got["error"] != "invalid_args" {
				t.Fatalf("unexpected response: %#v", got)
			}
		})
	}
}

func TestSSHErrorCodeClassifiesLocalFailures(t *testing.T) {
	if got := sshErrorCode(context.DeadlineExceeded, nil); got != "timeout" {
		t.Fatalf("timeout code = %q, want timeout", got)
	}

	if got := sshErrorCode(nil, &exec.Error{Name: "missing-ssh", Err: exec.ErrNotFound}); got != "ssh_missing" {
		t.Fatalf("missing binary code = %q, want ssh_missing", got)
	}

	if got := sshErrorCode(nil, errors.New("connection refused")); got != "connection_error" {
		t.Fatalf("connection error code = %q, want connection_error", got)
	}

	if got := sshErrorCode(nil, &exec.ExitError{}); got != "" {
		t.Fatalf("remote exit code = %q, want empty", got)
	}
}

func TestSSHResultErrorCodeClassifiesSSHExit255(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{name: "auth", stderr: "root@example.com: Permission denied (publickey).", want: "auth_failed"},
		{name: "host key", stderr: "Host key verification failed.", want: "host_key_failed"},
		{name: "connection", stderr: "ssh: connect to host example.com port 22: Connection refused", want: "connection_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sshResultErrorCode(nil, transport.Result{
				Stderr:   []byte(test.stderr),
				ExitCode: 255,
				Err:      &exec.ExitError{},
			})
			if got != test.want {
				t.Fatalf("sshResultErrorCode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSSHResultErrorCodeAllowsRemoteNonZero(t *testing.T) {
	got := sshResultErrorCode(nil, transport.Result{
		Stderr:   []byte("remote command failed"),
		ExitCode: 2,
		Err:      &exec.ExitError{},
	})
	if got != "" {
		t.Fatalf("sshResultErrorCode() = %q, want empty", got)
	}

	got = sshResultErrorCode(nil, transport.Result{
		Stderr:   []byte("application returned 255"),
		ExitCode: 255,
		Err:      &exec.ExitError{},
	})
	if got != "" {
		t.Fatalf("sshResultErrorCode(remote 255) = %q, want empty", got)
	}
}

func TestPasswordSSHErrorCodeClassifiesFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "missing ssh", err: passwordSSHError{err: &exec.Error{Name: "ssh", Err: exec.ErrNotFound}}, want: "ssh_missing"},
		{name: "timeout", err: passwordSSHError{err: context.DeadlineExceeded}, want: "timeout"},
		{name: "auth", err: passwordSSHError{output: []byte("Permission denied, please try again.")}, want: "auth_failed"},
		{name: "host key", err: passwordSSHError{output: []byte("Host key verification failed.")}, want: "host_key_failed"},
		{name: "generic", err: passwordSSHError{err: errors.New("ssh exited with status 255")}, want: "connection_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := passwordSSHErrorCode(test.err); got != test.want {
				t.Fatalf("passwordSSHErrorCode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunSSHWithPasswordClassifiesContextTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake ssh is unix-only")
	}
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh")
	if err := os.WriteFile(sshPath, []byte("#!/bin/sh\nsleep 1\n"), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := runSSHWithPassword(ctx, "password", []string{"example.com", "true"})
	if err == nil {
		t.Fatalf("runSSHWithPassword() error = nil, want timeout")
	}
	if got := passwordSSHErrorCode(err); got != "timeout" {
		t.Fatalf("passwordSSHErrorCode() = %q, want timeout; err = %v", got, err)
	}
}

func TestRunSSHWithPasswordClassifiesRemoteCommandFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake ssh is unix-only")
	}
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh")
	if err := os.WriteFile(sshPath, []byte("#!/bin/sh\necho remote chmod failed >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runSSHWithPassword(context.Background(), "password", []string{"example.com", "true"})
	if err == nil {
		t.Fatalf("runSSHWithPassword() error = nil, want command failure")
	}
	if got := passwordSSHErrorCode(err); got != "command_failed" {
		t.Fatalf("passwordSSHErrorCode() = %q, want command_failed; err = %v", got, err)
	}
}

func TestRunSSHWithPasswordPrioritizesRemoteExitOverAuthText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake ssh is unix-only")
	}
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh")
	if err := os.WriteFile(sshPath, []byte("#!/bin/sh\necho Permission denied >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runSSHWithPassword(context.Background(), "password", []string{"example.com", "true"})
	if err == nil {
		t.Fatalf("runSSHWithPassword() error = nil, want command failure")
	}
	if got := passwordSSHErrorCode(err); got != "command_failed" {
		t.Fatalf("passwordSSHErrorCode() = %q, want command_failed; err = %v", got, err)
	}
}

func TestKeyDeployRemoteCommandChainsAppendPath(t *testing.T) {
	got := keyDeployRemoteCommand("ssh-ed25519 AAAATEST")
	if !strings.Contains(got, "&& (") {
		t.Fatalf("keyDeployRemoteCommand() does not group append path: %s", got)
	}
	if !strings.Contains(got, ") && chmod 600") {
		t.Fatalf("keyDeployRemoteCommand() does not chain chmod after append group: %s", got)
	}
	if strings.Contains(got, "authorized_keys; chmod") {
		t.Fatalf("keyDeployRemoteCommand() can mask append failure: %s", got)
	}
}

func executeJSONError(t *testing.T, args []string) map[string]any {
	t.Helper()

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	return got
}
