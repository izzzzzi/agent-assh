package transport

import "testing"

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
	if !containsSubsequence(args, "--", "local file.txt", "root@example.com:'/tmp/remote file.txt'") {
		t.Fatalf("Args() = %#v, want local source and remote destination", args)
	}
}

func TestSCPCommandArgsForDownload(t *testing.T) {
	cmd := SCPCommand{
		Host: "example.com",
		User: "root",
	}

	args := cmd.Args("/var/log/app.log", "app.log", Download)

	if !containsSubsequence(args, "--", "root@example.com:'/var/log/app.log'", "app.log") {
		t.Fatalf("Args() = %#v, want remote source and local destination", args)
	}
}
