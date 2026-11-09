package transport

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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
	if !slices.Contains(args, "-T") {
		t.Fatalf("Args() = %#v, want -T to disable PTY allocation", args)
	}
	if slices.Contains(args, "-t") || slices.Contains(args, "-tt") {
		t.Fatalf("Args() = %#v, must not request PTY allocation", args)
	}
	if containsSubsequence(args, "sh", "-c") {
		t.Fatalf("Args() = %#v, must not use sh -c", args)
	}
	if !containsSubsequence(args, "--", "root@example.com", "echo hello") {
		t.Fatalf("Args() = %#v, want -- before destination to stop SSH option parsing", args)
	}
}

func TestSSHCommandArgsIncludesJump(t *testing.T) {
	cmd := SSHCommand{
		Host: "example.com",
		User: "root",
		Jump: "bastion.example.com",
	}

	args := cmd.Args("echo hello")

	if !containsSubsequence(args, "-J", "bastion.example.com") {
		t.Fatalf("Args() = %#v, want -J bastion.example.com", args)
	}
	if !containsSubsequence(args, "--", "root@example.com", "echo hello") {
		t.Fatalf("Args() = %#v, want target after --", args)
	}
	jumpIndex := indexOf(args, "-J")
	hostIndex := indexOf(args, "root@example.com")
	if jumpIndex == -1 || hostIndex == -1 || jumpIndex > hostIndex {
		t.Fatalf("Args() = %#v, want jump before target host", args)
	}
}

// TestArgsConcurrentControlPath guards the cached ControlMaster directory
// lookup. fleet exec builds SSH argument vectors from many goroutines at once,
// so the lookup must be synchronized; run with -race to exercise the guard.
func TestArgsConcurrentControlPath(t *testing.T) {
	if _, err := os.UserHomeDir(); err != nil {
		t.Skip("no home directory available; control path is not exercised")
	}

	const workers = 32
	cmd := SSHCommand{Host: "example.com", User: "root", Port: 2222}

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]string, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results[index] = strings.Join(cmd.Args("echo hello"), " ")
		}(i)
	}

	close(start)
	wg.Wait()

	for i, got := range results {
		if got != results[0] {
			t.Fatalf("Args() goroutine %d = %q, want %q", i, got, results[0])
		}
	}
	if !strings.Contains(results[0], "ControlPath=") {
		t.Fatalf("Args() = %q, want a ControlPath entry", results[0])
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
	stdout := strings.ReplaceAll(string(result.Stdout), "\r\n", "\n")
	if stdout != "stdout\n" {
		t.Fatalf("Run() Stdout = %q, want %q", result.Stdout, "stdout\n")
	}
}

// TestPTYArgsSendCommandOverStdin pins the PTY argument contract: -tt requests a
// PTY and the target must be the final argument, because Run feeds the command
// over stdin instead. Appending the command here would make the remote shell
// treat it as an argument to the login shell and never exit.
func TestPTYArgsSendCommandOverStdin(t *testing.T) {
	cmd := SSHCommand{Host: "example.com", User: "root", Port: 2222, ForcePTY: true}

	args := cmd.ptyArgs()

	if !slices.Contains(args, "-tt") {
		t.Fatalf("ptyArgs() = %#v, want -tt to request a PTY", args)
	}
	if slices.Contains(args, "-T") {
		t.Fatalf("ptyArgs() = %#v, must not disable PTY allocation", args)
	}
	if got := args[len(args)-1]; got != "root@example.com" {
		t.Fatalf("ptyArgs() = %#v, want the target as the final argument, got %q", args, got)
	}
	if !containsSubsequence(args, "--", "root@example.com") {
		t.Fatalf("ptyArgs() = %#v, want -- before the destination", args)
	}
}

// TestArgsWithForcePTYKeepsCommandAsArgument covers the ForcePTY branch of the
// shared argument builder: Args() always appends the command, and only
// ptyArgs() omits it. This branch was unreachable before the builders were
// merged, so it is worth pinning explicitly.
func TestArgsWithForcePTYKeepsCommandAsArgument(t *testing.T) {
	cmd := SSHCommand{Host: "example.com", User: "root", ForcePTY: true}

	args := cmd.Args("echo hello")

	if !slices.Contains(args, "-tt") {
		t.Fatalf("Args() = %#v, want -tt when ForcePTY is set", args)
	}
	if got := args[len(args)-1]; got != "echo hello" {
		t.Fatalf("Args() = %#v, want the command last, got %q", args, got)
	}
}

func TestTargetOmitsUserWhenEmpty(t *testing.T) {
	if got := (SSHCommand{Host: "example.com"}).target(); got != "example.com" {
		t.Fatalf("target() = %q, want %q", got, "example.com")
	}
	if got := (SSHCommand{Host: "example.com", User: "root"}).target(); got != "root@example.com" {
		t.Fatalf("target() = %q, want %q", got, "root@example.com")
	}
}

func TestStrictHostKeyCheckingMapsPolicies(t *testing.T) {
	tests := []struct {
		policy string
		want   string
	}{
		{"accept-new", "accept-new"},
		{"strict", "yes"},
		{"no-check", "no"},
		{"", ""},
		{"unknown", ""},
	}

	for _, tt := range tests {
		if got := strictHostKeyChecking(tt.policy); got != tt.want {
			t.Fatalf("strictHostKeyChecking(%q) = %q, want %q", tt.policy, got, tt.want)
		}
	}
}

func TestControlPathHonoursExplicitOverride(t *testing.T) {
	cmd := SSHCommand{Host: "example.com", ControlPath: "/tmp/custom.sock"}

	if got := cmd.controlPath(); got != "/tmp/custom.sock" {
		t.Fatalf("controlPath() = %q, want the explicit override", got)
	}
}

func TestControlPathIsStablePerTarget(t *testing.T) {
	first := SSHCommand{Host: "example.com", User: "root"}
	second := SSHCommand{Host: "example.com", User: "root"}
	other := SSHCommand{Host: "example.com", User: "deploy"}

	if first.controlPath() == "" {
		t.Skip("no home directory available; control path is not exercised")
	}
	if first.controlPath() != second.controlPath() {
		t.Fatal("controlPath() is not stable for the same target")
	}
	if first.controlPath() == other.controlPath() {
		t.Fatal("controlPath() collides across different targets")
	}
}

// TestRunWithForcePTYDeliversCommandOverStdin is the end-to-end guard for the
// PTY path, which the review flagged as the most fragile transport branch. The
// mock fails if the command shows up in argv or if stdin arrives empty.
func TestRunWithForcePTYDeliversCommandOverStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the PTY mock is a POSIX shell script")
	}

	dir := t.TempDir()
	mockPath := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"echo hello\" ]; then\n" +
		"    echo \"command leaked into argv\" >&2\n" +
		"    exit 8\n" +
		"  fi\n" +
		"done\n" +
		"payload=$(cat)\n" +
		"if [ -z \"$payload\" ]; then\n" +
		"  echo \"stdin was empty\" >&2\n" +
		"  exit 9\n" +
		"fi\n" +
		"echo MARKER\n"
	if err := os.WriteFile(mockPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Binary is left empty so Run must fall back to the "ssh" name on PATH.
	cmd := SSHCommand{Host: "example.com", User: "root", ForcePTY: true}
	result := cmd.Run(context.Background(), "echo hello")

	if result.ExitCode != 0 {
		t.Fatalf("Run() ExitCode = %d, want 0; stderr = %q; err = %v",
			result.ExitCode, result.Stderr, result.Err)
	}
	if got := string(result.Stdout); got != "MARKER" {
		t.Fatalf("Run() Stdout = %q, want %q", got, "MARKER")
	}
}

func TestBatchModeIsOptIn(t *testing.T) {
	plain := SSHCommand{Host: "example.com", User: "root"}
	if args := plain.Args("echo hi"); slices.Contains(args, "BatchMode=yes") {
		t.Fatalf("Args() = %#v, must not set BatchMode unless asked", args)
	}

	batched := SSHCommand{Host: "example.com", User: "root", Batch: true}
	if !containsSubsequence(batched.Args("echo hi"), "-o", "BatchMode=yes") {
		t.Fatalf("Args() = %#v, want -o BatchMode=yes", batched.Args("echo hi"))
	}
}

func TestSCPBatchModeIsOptIn(t *testing.T) {
	plain := SCPCommand{Host: "example.com", User: "root"}
	if args := plain.Args("/a", "/b", Upload); slices.Contains(args, "BatchMode=yes") {
		t.Fatalf("Args() = %#v, must not set BatchMode unless asked", args)
	}

	batched := SCPCommand{Host: "example.com", User: "root", Batch: true}
	if !containsSubsequence(batched.Args("/a", "/b", Upload), "-o", "BatchMode=yes") {
		t.Fatalf("Args() = %#v, want -o BatchMode=yes", batched.Args("/a", "/b", Upload))
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

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
