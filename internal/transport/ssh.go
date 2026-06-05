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
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

func (c SSHCommand) Args(remoteCommand string) []string {
	args := make([]string, 0, 14)

	args = append(args, "-T")
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
	if value := strictHostKeyChecking(c.HostKeyPolicy); value != "" {
		args = append(args, "-o", "StrictHostKeyChecking="+value)
	}
	if cp := c.controlPath(); cp != "" {
		args = append(args, "-o", "ControlMaster=auto")
		args = append(args, "-o", "ControlPath="+cp)
		args = append(args, "-o", "ControlPersist=300")
	}

	args = append(args, "--", c.target(), remoteCommand)
	return args
}

var sshControlDir string

func controlDir() string {
	if sshControlDir != "" {
		return sshControlDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".ssh", "controlmasters")
	_ = os.MkdirAll(dir, 0700)
	sshControlDir = dir
	return dir
}

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

	cmd := exec.CommandContext(ctx, binary, c.Args(remoteCommand)...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
		Err:      err,
	}
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
