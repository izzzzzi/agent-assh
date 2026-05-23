package transport

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"

	"github.com/izzzzzi/agent-assh/internal/remote"
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
}

func (c SCPCommand) Args(source string, destination string, direction SCPDirection) []string {
	args := make([]string, 0, 12)
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
	return target + ":" + remote.SingleQuote(path)
}
