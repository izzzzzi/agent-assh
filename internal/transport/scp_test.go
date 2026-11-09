package transport

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSCPCommandArgsForUploadIncludeSharedSSHOptions(t *testing.T) {
	cmd := SCPCommand{
		Host:          "example.com",
		User:          "root",
		Port:          2222,
		Identity:      "key",
		Jump:          "bastion.example.com",
		TimeoutSecond: 30,
		HostKeyPolicy: "strict",
	}

	args := cmd.Args("local file.txt", "/tmp/remote file.txt", Upload)

	if !containsSubsequence(args, "-P", "2222") {
		t.Fatalf("Args() = %#v, want scp port flag", args)
	}
	if !containsSubsequence(args, "-i", "key") {
		t.Fatalf("Args() = %#v, want identity flag", args)
	}
	if !containsSubsequence(args, "-J", "bastion.example.com") {
		t.Fatalf("Args() = %#v, want jump host flag", args)
	}
	if !containsSubsequence(args, "-o", "ConnectTimeout=30") {
		t.Fatalf("Args() = %#v, want timeout option", args)
	}
	if !containsSubsequence(args, "-o", "StrictHostKeyChecking=yes") {
		t.Fatalf("Args() = %#v, want strict host key option", args)
	}
	// scp arguments are passed via execve (no shell), so the remote path
	// must NOT be shell-escaped. Wrapping it in single quotes makes the
	// quotes part of the literal filename: scp parses host:'/tmp/...' as
	// path '/tmp/...' (with quotes), producing files named 'file.txt'
	// instead of file.txt, and "No such file or directory" for absolute
	// paths. See bug: transfer put adds single quotes to dest filename.
	if !containsSubsequence(args, "--", "local file.txt", "root@example.com:/tmp/remote file.txt") {
		t.Fatalf("Args() = %#v, want unquoted remote destination (execve, no shell)", args)
	}
}

func TestSCPCommandArgsForUploadAbsolutePathNotSingleQuoted(t *testing.T) {
	cmd := SCPCommand{
		Host: "example.com",
		User: "root",
	}

	// Regression: absolute paths used to arrive at scp as
	// root@example.com:'/tmp/file.php' and create a file named
	// 'file.php' (with literal quotes) or fail outright.
	args := cmd.Args("local.php", "/tmp/file.php", Upload)

	if !containsSubsequence(args, "--", "local.php", "root@example.com:/tmp/file.php") {
		t.Fatalf("Args() = %#v, want root@example.com:/tmp/file.php without single quotes", args)
	}
	for _, a := range args {
		if strings.Contains(a, "'/tmp/file.php'") || strings.Contains(a, `'example.com:'`) {
			t.Fatalf("Args() = %#v, remote path must not be single-quoted", args)
		}
	}
}

func TestSCPCommandArgsForDownload(t *testing.T) {
	cmd := SCPCommand{
		Host: "example.com",
		User: "root",
	}

	args := cmd.Args("/var/log/app.log", "app.log", Download)

	// Remote source path is execve argv, not shell — no single quotes.
	if !containsSubsequence(args, "--", "root@example.com:/var/log/app.log", "app.log") {
		t.Fatalf("Args() = %#v, want unquoted remote source (execve, no shell)", args)
	}
}

// writeSCPMock drops an executable shell script named scp into a temp dir and
// prepends it to PATH, so SCPCommand.Run resolves the mock through the default
// binary name as well as an explicit Binary value.
func writeSCPMock(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the scp mock is a POSIX shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scp"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSCPCommandRunReportsExitCodeAndStreams(t *testing.T) {
	writeSCPMock(t, "#!/bin/sh\necho 'sent 1 file'\necho 'lost connection' >&2\nexit 5\n")

	cmd := SCPCommand{Binary: "scp", Host: "example.com", User: "root"}
	result := cmd.Run(context.Background(), "/tmp/src", "/tmp/dst", Upload)

	if result.ExitCode != 5 {
		t.Fatalf("Run() ExitCode = %d, want 5", result.ExitCode)
	}
	if result.Err == nil {
		t.Fatal("Run() Err = nil, want an exit error")
	}
	if !strings.Contains(string(result.Stdout), "sent 1 file") {
		t.Fatalf("Run() Stdout = %q, want the mock's stdout", result.Stdout)
	}
	if !strings.Contains(string(result.Stderr), "lost connection") {
		t.Fatalf("Run() Stderr = %q, want the mock's stderr", result.Stderr)
	}
}

func TestSCPCommandRunDefaultsBinaryAndSucceeds(t *testing.T) {
	writeSCPMock(t, "#!/bin/sh\nexit 0\n")

	// Binary is left empty so Run must fall back to the "scp" name on PATH.
	cmd := SCPCommand{Host: "example.com", User: "root"}
	result := cmd.Run(context.Background(), "/tmp/src", "/tmp/dst", Upload)

	if result.ExitCode != 0 {
		t.Fatalf("Run() ExitCode = %d, want 0; err = %v", result.ExitCode, result.Err)
	}
	if result.Err != nil {
		t.Fatalf("Run() Err = %v, want nil", result.Err)
	}
}

func TestSCPCommandRunReportsMissingBinary(t *testing.T) {
	cmd := SCPCommand{Binary: "assh-no-such-binary", Host: "example.com", User: "root"}
	result := cmd.Run(context.Background(), "/tmp/src", "/tmp/dst", Upload)

	if result.ExitCode != -1 {
		t.Fatalf("Run() ExitCode = %d, want -1 when the binary cannot be started", result.ExitCode)
	}
	if result.Err == nil {
		t.Fatal("Run() Err = nil, want a start error")
	}
}
