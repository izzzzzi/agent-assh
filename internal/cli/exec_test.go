package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"
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
