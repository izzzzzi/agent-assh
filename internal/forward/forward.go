package forward

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/izzzzzi/agent-assh/internal/transport"
)

type Spec struct {
	Host          string
	User          string
	Port          int
	Identity      string
	Jump          string
	TimeoutSecond int
	HostKeyPolicy string
	ControlSocket string
	Persist       time.Duration
	Local         []string
	Remote        []string
	Dynamic       []string
}

func ControlSocketPath(baseDir string, name string) string {
	return filepath.Join(baseDir, "forward", "sockets", name+".sock")
}

func StartArgs(spec Spec) []string {
	args := commonArgs(spec)
	args = append(args, "-N", "-f", "-M", "-S", spec.ControlSocket)
	if spec.Persist > 0 {
		args = append(args, "-o", "ControlPersist="+spec.Persist.String())
	}
	for _, rule := range spec.Local {
		args = append(args, "-L", rule)
	}
	for _, rule := range spec.Remote {
		args = append(args, "-R", rule)
	}
	for _, rule := range spec.Dynamic {
		args = append(args, "-D", rule)
	}
	args = append(args, "--", target(spec.User, spec.Host))
	return args
}

func ControlArgs(spec Spec, operation string) []string {
	args := commonArgs(spec)
	args = append(args, "-S", spec.ControlSocket, "-O", operation, "--", target(spec.User, spec.Host))
	return args
}

func Command(spec Spec) transport.SSHCommand {
	return transport.SSHCommand{
		Host:          spec.Host,
		User:          spec.User,
		Port:          spec.Port,
		Identity:      spec.Identity,
		Jump:          spec.Jump,
		TimeoutSecond: spec.TimeoutSecond,
		HostKeyPolicy: spec.HostKeyPolicy,
	}
}

func commonArgs(spec Spec) []string {
	args := make([]string, 0, 16)
	if spec.Port != 0 && spec.Port != 22 {
		args = append(args, "-p", strconv.Itoa(spec.Port))
	}
	if spec.Identity != "" {
		args = append(args, "-i", spec.Identity)
	}
	if spec.Jump != "" {
		args = append(args, "-J", spec.Jump)
	}
	if spec.TimeoutSecond > 0 {
		args = append(args, "-o", "ConnectTimeout="+strconv.Itoa(spec.TimeoutSecond))
	}
	if value := strictHostKeyChecking(spec.HostKeyPolicy); value != "" {
		args = append(args, "-o", "StrictHostKeyChecking="+value)
	}
	return args
}

func target(user string, host string) string {
	if user == "" {
		return host
	}
	return user + "@" + host
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
