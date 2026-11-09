package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type SSHCommand struct {
	Binary        string
	Host          string
	User          string
	Port          int
	Identity      string
	Jump          string
	TimeoutSecond int
	HostKeyPolicy string
	ControlPath   string
	ForcePTY      bool
	// Batch adds -o BatchMode=yes, which stops ssh from prompting for a
	// password or key passphrase. Agents set this so a missing credential fails
	// fast instead of blocking on a prompt no one can answer.
	Batch bool
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

// Args builds the OpenSSH argument vector with the remote command appended as
// the final argument. Callers driving an interactive PTY must use ptyArgs
// instead: with -tt the command travels over stdin, not as an argument.
func (c SSHCommand) Args(remoteCommand string) []string {
	return c.buildArgs(remoteCommand, true)
}

// buildArgs assembles the OpenSSH argument vector. When includeCommand is true
// the remote command is appended after the destination; PTY mode leaves it out
// because Run delivers the command over stdin instead.
func (c SSHCommand) buildArgs(remoteCommand string, includeCommand bool) []string {
	args := make([]string, 0, 16)

	if c.ForcePTY {
		args = append(args, "-tt")
	} else {
		args = append(args, "-T")
	}
	if c.Port != 0 && c.Port != 22 {
		args = append(args, "-p", strconv.Itoa(c.Port))
	}
	if c.Identity != "" {
		args = append(args, "-i", c.Identity)
	}
	if c.Jump != "" {
		args = append(args, "-J", c.Jump)
	}
	if c.TimeoutSecond > 0 {
		args = append(args, "-o", "ConnectTimeout="+strconv.Itoa(c.TimeoutSecond))
	}
	if c.Batch {
		args = append(args, "-o", "BatchMode=yes")
	}
	if value := strictHostKeyChecking(c.HostKeyPolicy); value != "" {
		args = append(args, "-o", "StrictHostKeyChecking="+value)
	}
	if cp := c.controlPath(); cp != "" {
		args = append(args, "-o", "ControlMaster=auto", "-o", "ControlPath="+cp, "-o", "ControlPersist=300")
	}

	args = append(args, "--", c.target())
	if includeCommand {
		args = append(args, remoteCommand)
	}
	return args
}

// controlDir returns the directory holding ControlMaster sockets, creating it on
// first use. sync.OnceValue keeps the cached lookup race-free: fleet exec builds
// SSH argument vectors from many goroutines at once.
var controlDir = sync.OnceValue(func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".ssh", "controlmasters")
	_ = os.MkdirAll(dir, 0o700)
	return dir
})

func (c SSHCommand) controlPath() string {
	if c.ControlPath != "" {
		return c.ControlPath
	}
	dir := controlDir()
	if dir == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(c.target()))
	return filepath.Join(dir, fmt.Sprintf("assh-%x.sock", hash[:8]))
}

func (c SSHCommand) Run(ctx context.Context, remoteCommand string) Result {
	binary := c.Binary
	if binary == "" {
		binary = "ssh"
	}

	// With -tt the remote command must travel over stdin, not as an SSH
	// argument: ssh -tt hands it to the remote shell, which does not exit after
	// the command completes.
	var args []string
	if c.ForcePTY {
		args = c.ptyArgs()
	} else {
		args = c.Args(remoteCommand)
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if c.ForcePTY {
		// With -tt the command must be sent via stdin (not SSH arg), followed
		// by exit so the interactive shell closes after the command.
		cmd.Stdin = strings.NewReader(ptyScript(remoteCommand))
	}

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	result := Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
		Err:      err,
	}

	if c.ForcePTY {
		result.Stdout = CleanPTYOutput(result.Stdout, "{ "+remoteCommand)
		result.Stderr = CleanPTYOutput(result.Stderr, "")
	}

	return result
}

// ptyScript wraps the command so that `exit` is parsed together with it. Sent
// as a separate line it sits in the terminal as type-ahead, and a command that
// reads the terminal swallows it and leaves the shell waiting forever: sudo
// does exactly that where use_pty is on, the default on current Ubuntu.
func ptyScript(remoteCommand string) string {
	return "{ " + remoteCommand + "\n}; exit\n"
}

// ptyArgs builds the argument vector for PTY mode, where Run delivers the remote
// command over stdin rather than as a trailing argument.
func (c SSHCommand) ptyArgs() []string {
	return c.buildArgs("", false)
}

func (c SSHCommand) target() string {
	if c.User == "" {
		return c.Host
	}
	return c.User + "@" + c.Host
}

func strictHostKeyChecking(policy string) string {
	switch policy {
	case "accept-new":
		return "accept-new"
	case "strict":
		return "yes"
	case "no-check":
		return "no"
	default:
		return ""
	}
}
