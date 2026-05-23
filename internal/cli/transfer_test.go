package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestRootIncludesTransferCommand(t *testing.T) {
	cmd := NewRootCommand()
	found, _, err := cmd.Find([]string{"transfer"})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if found == nil || found.Name() != "transfer" {
		t.Fatalf("Find(transfer) = %v, want transfer command", found)
	}
}

func TestTransferPutRunsSCPAndReturnsJSON(t *testing.T) {
	source := filepath.Join(t.TempDir(), "local file.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(_ context.Context, command transport.SCPCommand, src string, dst string, direction transport.SCPDirection) transport.Result {
		if direction != transport.Upload {
			t.Fatalf("direction=%v, want upload", direction)
		}
		if command.Host != "example.com" || command.User != "root" || command.Jump != "bastion.example.com" {
			t.Fatalf("command=%#v", command)
		}
		if src != source || dst != "/tmp/remote file.txt" {
			t.Fatalf("src=%q dst=%q", src, dst)
		}
		return transport.Result{ExitCode: 0}
	}

	got := executeTransferJSON(t, []string{"transfer", "put", "--host", "example.com", "--jump", "bastion.example.com", source, "/tmp/remote file.txt"})

	if got["ok"] != true || got["host"] != "example.com" || got["user"] != "root" || got["source"] != source || got["destination"] != "/tmp/remote file.txt" || got["bytes"] != float64(5) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestTransferGetRunsSCPAndReturnsDownloadedSize(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "downloaded file.txt")

	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(_ context.Context, command transport.SCPCommand, src string, dst string, direction transport.SCPDirection) transport.Result {
		if direction != transport.Download {
			t.Fatalf("direction=%v, want download", direction)
		}
		if command.Host != "example.com" || command.User != "deploy" || command.Port != 2222 {
			t.Fatalf("command=%#v", command)
		}
		if src != "/var/log/app.log" || dst != destination {
			t.Fatalf("src=%q dst=%q", src, dst)
		}
		if err := os.WriteFile(destination, []byte("downloaded"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		return transport.Result{ExitCode: 0}
	}

	got := executeTransferJSON(t, []string{"transfer", "get", "--host", "example.com", "--user", "deploy", "--port", "2222", "/var/log/app.log", destination})

	if got["ok"] != true || got["host"] != "example.com" || got["user"] != "deploy" || got["source"] != "/var/log/app.log" || got["destination"] != destination || got["bytes"] != float64(10) {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestTransferValidatesArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "subcommand", args: []string{"transfer"}, want: "transfer subcommand required"},
		{name: "put args", args: []string{"transfer", "put", "--host", "example.com", "only-source"}, want: "source and destination required"},
		{name: "get args", args: []string{"transfer", "get", "--host", "example.com", "src", "dst", "extra"}, want: "source and destination required"},
		{name: "host", args: []string{"transfer", "put", "src", "dst"}, want: "host required"},
		{name: "port", args: []string{"transfer", "put", "--host", "example.com", "--port", "0", "src", "dst"}, want: "port must be between 1 and 65535"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := executeTransferJSONError(t, test.args)
			if got["error"] != "invalid_args" || got["message"] != test.want {
				t.Fatalf("unexpected response: %#v", got)
			}
		})
	}
}

func TestTransferPutRejectsMissingLocalSource(t *testing.T) {
	got := executeTransferJSONError(t, []string{"transfer", "put", "--host", "example.com", filepath.Join(t.TempDir(), "missing.txt"), "/tmp/missing.txt"})
	if got["error"] != "invalid_args" || !strings.Contains(got["message"].(string), "local source") {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestTransferMapsSCPErrorsToJSON(t *testing.T) {
	source := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(context.Context, transport.SCPCommand, string, string, transport.SCPDirection) transport.Result {
		return transport.Result{
			Stderr:   []byte("scp: Permission denied"),
			ExitCode: 255,
			Err:      errors.New("scp failed"),
		}
	}

	got := executeTransferJSONError(t, []string{"transfer", "put", "--host", "example.com", source, "/tmp/file.txt"})
	if got["error"] != "auth_failed" || got["message"] != "scp: Permission denied" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestPromptManifestIncludesTransferCommands(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	body := out.String()
	for _, want := range []string{"assh transfer put", "assh transfer get"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt manifest missing %q in %s", want, body)
		}
	}
}

func executeTransferJSON(t *testing.T, args []string) map[string]any {
	t.Helper()

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, output = %q", err, out.String())
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	return got
}

func executeTransferJSONError(t *testing.T, args []string) map[string]any {
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
