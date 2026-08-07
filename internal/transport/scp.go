package transport

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
)

type SCPDirection int

const (
	Upload SCPDirection = iota
	Download
)

type SCPCommand struct {
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

func (c SCPCommand) Args(source string, destination string, direction SCPDirection) []string {
	args := make([]string, 0, 16)
	if c.Port != 0 && c.Port != 22 {
		args = append(args, "-P", strconv.Itoa(c.Port))
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
	if c.ControlPath != "" {
		args = append(args, "-o", "ControlPath="+c.ControlPath)
	}

	if direction == Download {
		source = c.remotePath(source)
	} else {
		destination = c.remotePath(destination)
	}
	args = append(args, "--", source, destination)
	return args
}

func (c SCPCommand) Run(ctx context.Context, source string, destination string, direction SCPDirection) Result {
	binary := c.Binary
	if binary == "" {
		binary = "scp"
	}

	cmd := exec.CommandContext(ctx, binary, c.Args(source, destination, direction)...)
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

func (c SCPCommand) remotePath(path string) string {
	target := c.Host
	if c.User != "" {
		target = c.User + "@" + target
	}
	// scp argv is passed via execve (see Run), not through a shell, so the
	// remote path must NOT be shell-escaped. Single-quoting it makes the
	// quotes part of the literal filename: scp parses host:'/tmp/x' as the
	// path '/tmp/x' (with quotes), producing files named 'x' instead of x
	// for relative targets and "No such file or directory" for absolute ones.
	return target + ":" + path
}
