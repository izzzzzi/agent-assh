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
	runSCP = func(_ context.Context, command transport.SCPCommand, src, dst string,
		direction transport.SCPDirection) transport.Result {
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
	stubSuccessfulMkdir(t)

	args := []string{
		"transfer", "put", "--host", "example.com",
		"--jump", "bastion.example.com", source, "/tmp/remote file.txt",
	}
	got := executeTransferJSON(t, args)

	if got["ok"] != true || got["host"] != "example.com" || got["user"] != "root" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["source"] != source || got["destination"] != "/tmp/remote file.txt" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["bytes"] != float64(5) {
		t.Fatalf("unexpected bytes: %#v", got["bytes"])
	}
}

func TestTransferGetRunsSCPAndReturnsDownloadedSize(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "downloaded file.txt")

	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(_ context.Context, command transport.SCPCommand, src, dst string,
		direction transport.SCPDirection) transport.Result {
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

	args := []string{
		"transfer", "get", "--host", "example.com",
		"--user", "deploy", "--port", "2222",
		"/var/log/app.log", destination,
	}
	got := executeTransferJSON(t, args)

	if got["ok"] != true || got["host"] != "example.com" || got["user"] != "deploy" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["source"] != "/var/log/app.log" || got["destination"] != destination {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["bytes"] != float64(10) {
		t.Fatalf("unexpected bytes: %#v", got["bytes"])
	}
}

func TestTransferValidatesArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "subcommand", args: []string{"transfer"}, want: "transfer subcommand required"},
		{
			name: "put args",
			args: []string{"transfer", "put", "--host", "example.com", "only-source"},
			want: "source and destination required",
		},
		{
			name: "get args",
			args: []string{"transfer", "get", "--host", "example.com", "src", "dst", "extra"},
			want: "source and destination required",
		},
		{name: "host", args: []string{"transfer", "put", "src", "dst"}, want: "host required"},
		{
			name: "port",
			args: []string{"transfer", "put", "--host", "example.com", "--port", "0", "src", "dst"},
			want: "port must be between 1 and 65535",
		},
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
	missing := filepath.Join(t.TempDir(), "missing.txt")
	args := []string{"transfer", "put", "--host", "example.com", missing, "/tmp/missing.txt"}
	got := executeTransferJSONError(t, args)
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
	stubSuccessfulMkdir(t)

	got := executeTransferJSONError(t, []string{"transfer", "put", "--host", "example.com", source, "/tmp/file.txt"})
	if got["error"] != "auth_failed" || got["message"] != "scp: Permission denied" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestRemoteUploadParent(t *testing.T) {
	tests := []struct {
		dest   string
		parent string
		ok     bool
	}{
		{"/tmp/file.txt", "/tmp", true},
		{"/var/www/html/console/migrations/", "/var/www/html/console/migrations", true},
		{"/opt/app/uploads/", "/opt/app/uploads", true},
		{"~/deploy/infra/", "~/deploy/infra", true},
		{"~/deploy/file", "~/deploy", true},
		{"file.txt", "", false},
		{"/file.txt", "", false},
		{"/", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		parent, ok := remoteUploadParent(tt.dest)
		if ok != tt.ok || (tt.ok && parent != tt.parent) {
			t.Fatalf("remoteUploadParent(%q) = %q, %v; want %q, %v", tt.dest, parent, ok, tt.parent, tt.ok)
		}
	}
}

func TestRemoteMkdirCommandExpandsTilde(t *testing.T) {
	if got := remoteMkdirCommand("~/deploy/infra"); got != "mkdir -p ~/'deploy/infra'" {
		t.Fatalf("remoteMkdirCommand(~...) = %q", got)
	}
	if got := remoteMkdirCommand("/opt/app"); got != "mkdir -p '/opt/app'" {
		t.Fatalf("remoteMkdirCommand(/opt/app) = %q", got)
	}
	if got := remoteMkdirCommand("~/my dir"); got != "mkdir -p ~/'my dir'" {
		t.Fatalf("remoteMkdirCommand(~/my dir) = %q", got)
	}
}

func TestTransferPutCreatesRemoteParentDirBeforeSCP(t *testing.T) {
	source := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var mkdirCommand string
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, _ transport.SSHCommand, remoteCommand string) transport.Result {
		mkdirCommand = remoteCommand
		return transport.Result{ExitCode: 0}
	}
	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(context.Context, transport.SCPCommand, string, string, transport.SCPDirection) transport.Result {
		return transport.Result{ExitCode: 0}
	}

	got := executeTransferJSON(t, []string{
		"transfer", "put", "--host", "example.com",
		source, "/opt/app/uploads/file.txt",
	})
	if got["ok"] != true {
		t.Fatalf("unexpected response: %#v", got)
	}
	if mkdirCommand != "mkdir -p '/opt/app/uploads'" {
		t.Fatalf("mkdir command = %q", mkdirCommand)
	}
}

func TestTransferPutTrailingSlashCreatesDirectory(t *testing.T) {
	source := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var mkdirCommand string
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, _ transport.SSHCommand, remoteCommand string) transport.Result {
		mkdirCommand = remoteCommand
		return transport.Result{ExitCode: 0}
	}
	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(context.Context, transport.SCPCommand, string, string, transport.SCPDirection) transport.Result {
		return transport.Result{ExitCode: 0}
	}

	executeTransferJSON(t, []string{
		"transfer", "put", "--host", "example.com",
		source, "/var/www/html/console/migrations/",
	})
	if mkdirCommand != "mkdir -p '/var/www/html/console/migrations'" {
		t.Fatalf("mkdir command = %q", mkdirCommand)
	}
}

func TestTransferPutMkdirFailureIsReported(t *testing.T) {
	source := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{Stderr: []byte("mkdir: cannot create directory '/opt': Permission denied"), ExitCode: 1}
	}

	got := executeTransferJSONError(t, []string{
		"transfer", "put", "--host", "example.com",
		source, "/opt/app/file.txt",
	})
	if got["error"] != "mkdir_failed" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestParseRemoteSpec(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		wantUser string
		wantHost string
		wantPath string
		wantOK   bool
	}{
		{"host and path", "example.com:/tmp/file", "", "example.com", "/tmp/file", true},
		{"user host and path", "root@example.com:/tmp/file", "root", "example.com", "/tmp/file", true},
		{"user host relative path", "deploy@host:app.log", "deploy", "host", "app.log", true},
		{"host only in host part with port-like path", "host:8080", "", "host", "8080", true},
		{"plain local path", "/tmp/file", "", "", "", false},
		{"relative local path", "file.txt", "", "", "", false},
		{"local path containing a colon", "/tmp/a:b", "", "", "", false},
		{"relative path containing a colon", "logs/a:b.txt", "", "", "", false},
		{"empty path after colon", "host:", "", "", "", false},
		{"empty host before colon", ":path", "", "", "", false},
		{"empty user before at", "@host:path", "", "host", "path", true},
		{"windows drive letter", `C:\Users\me\file.txt`, "", "", "", false},
		{"windows drive with forward slash", "C:/Users/me/file.txt", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, host, path, ok := parseRemoteSpec(tt.spec)
			if ok != tt.wantOK {
				t.Fatalf("parseRemoteSpec(%q) ok = %v, want %v", tt.spec, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if user != tt.wantUser || host != tt.wantHost || path != tt.wantPath {
				t.Fatalf("parseRemoteSpec(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.spec, user, host, path, tt.wantUser, tt.wantHost, tt.wantPath)
			}
		})
	}
}

func TestTransferPutAcceptsSCPStyleRemoteSpec(t *testing.T) {
	source := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(_ context.Context, command transport.SCPCommand, src, dst string,
		direction transport.SCPDirection) transport.Result {
		if command.Host != "example.com" || command.User != "root" {
			t.Fatalf("command=%#v, want host/user taken from the scp-style argument", command)
		}
		if src != source || dst != "/var/www/index.html" {
			t.Fatalf("src=%q dst=%q, want the host stripped from the remote path", src, dst)
		}
		return transport.Result{ExitCode: 0}
	}
	stubSuccessfulMkdir(t)

	// No --host: the destination carries it, exactly as an scp recipe would.
	got := executeTransferJSON(t, []string{
		"transfer", "put", source, "root@example.com:/var/www/index.html",
	})

	if got["ok"] != true || got["host"] != "example.com" || got["user"] != "root" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["destination"] != "/var/www/index.html" {
		t.Fatalf("unexpected destination: %#v", got["destination"])
	}
}

func TestTransferGetAcceptsSCPStyleRemoteSpec(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "local.txt")

	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(_ context.Context, command transport.SCPCommand, src, dst string,
		direction transport.SCPDirection) transport.Result {
		if direction != transport.Download {
			t.Fatalf("direction = %v, want download", direction)
		}
		if command.Host != "example.com" || command.User != "deploy" {
			t.Fatalf("command=%#v, want host/user taken from the scp-style argument", command)
		}
		if src != "/var/log/app.log" {
			t.Fatalf("src=%q, want the host stripped from the remote path", src)
		}
		if err := os.WriteFile(dst, []byte("downloaded"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		return transport.Result{ExitCode: 0}
	}

	got := executeTransferJSON(t, []string{
		"transfer", "get", "deploy@example.com:/var/log/app.log", destination,
	})

	if got["ok"] != true || got["host"] != "example.com" || got["user"] != "deploy" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["source"] != "/var/log/app.log" {
		t.Fatalf("unexpected source: %#v", got["source"])
	}
}

func TestTransferExplicitHostWinsOverArgument(t *testing.T) {
	source := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldRunSCP := runSCP
	t.Cleanup(func() { runSCP = oldRunSCP })
	runSCP = func(_ context.Context, command transport.SCPCommand, src, dst string,
		direction transport.SCPDirection) transport.Result {
		if command.Host != "explicit.example.com" {
			t.Fatalf("command.Host = %q, want the explicit --host to win", command.Host)
		}
		return transport.Result{ExitCode: 0}
	}
	stubSuccessfulMkdir(t)

	// -H is given, so the colon in the destination must be left alone.
	got := executeTransferJSON(t, []string{
		"transfer", "put", "--host", "explicit.example.com", source, "/tmp/odd:name",
	})

	if got["ok"] != true || got["host"] != "explicit.example.com" {
		t.Fatalf("unexpected response: %#v", got)
	}
	if got["destination"] != "/tmp/odd:name" {
		t.Fatalf("destination = %#v, want it preserved verbatim", got["destination"])
	}
}

func TestTransferWithoutHostSuggestsSCPForm(t *testing.T) {
	source := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := executeTransferJSONError(t, []string{"transfer", "put", source, "/tmp/remote.txt"})

	if got["message"] != "host required" {
		t.Fatalf("unexpected response: %#v", got)
	}
	hint, _ := got["hint"].(string)
	if !strings.Contains(hint, "user@host:/remote/path") {
		t.Fatalf("hint %q should show the scp form", hint)
	}
}

func stubSuccessfulMkdir(t *testing.T) {
	t.Helper()
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(context.Context, transport.SSHCommand, string) transport.Result {
		return transport.Result{ExitCode: 0}
	}
}

func TestRsyncSSHCommandQuotesArguments(t *testing.T) {
	got := rsyncSSHCommand(sshOptions{
		Port:          2200,
		Identity:      "/Users/me/key with spaces';touch pwned'",
		Jump:          "jump host.example.com",
		HostKeyPolicy: "strict",
	})

	for _, want := range []string{
		"ssh -T",
		"-p 2200",
		`-i '/Users/me/key with spaces'"'"';touch pwned'"'"''`,
		`-J 'jump host.example.com'`,
		"-o StrictHostKeyChecking=yes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rsyncSSHCommand() = %q, missing %q", got, want)
		}
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
