package transport

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestSSHCommandBuildsArgvWithoutShell(t *testing.T) {
	cmd := SSHCommand{
		Binary:        "ssh",
		Host:          "example.com",
		User:          "root",
		Port:          2222,
		Identity:      "key",
		HostKeyPolicy: "strict",
	}

	args := cmd.Args("echo hello")

	if !containsSubsequence(args, "-p", "2222") {
		t.Fatalf("Args() = %#v, want -p 2222", args)
	}
	if containsSubsequence(args, "sh", "-c") {
		t.Fatalf("Args() = %#v, must not use sh -c", args)
	}
}

func TestRunUsesMockSSH(t *testing.T) {
	dir := t.TempDir()
	name := "ssh"
	if runtime.GOOS == "windows" {
		name = "ssh.bat"
	}

	mockPath := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(mockPath, []byte("@echo stdout\r\n@echo stderr 1>&2\r\nexit /B 7\r\n"), 0o755); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	} else {
		if err := os.WriteFile(mockPath, []byte("#!/bin/sh\necho stdout\necho stderr >&2\nexit 7\n"), 0o755); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := SSHCommand{Binary: name, Host: "example.com", User: "root"}.Run(context.Background(), "ignored")

	if result.ExitCode != 7 {
		t.Fatalf("Run() ExitCode = %d, want 7; err = %v", result.ExitCode, result.Err)
	}
	if string(result.Stdout) != "stdout\n" {
		t.Fatalf("Run() Stdout = %q, want %q", result.Stdout, "stdout\n")
	}
}

func containsSubsequence(values []string, subsequence ...string) bool {
	if len(subsequence) == 0 {
		return true
	}
	for i := 0; i <= len(values)-len(subsequence); i++ {
		if slices.Equal(values[i:i+len(subsequence)], subsequence) {
			return true
		}
	}
	return false
}
