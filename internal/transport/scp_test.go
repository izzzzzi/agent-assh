package transport

import (
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
