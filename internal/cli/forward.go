package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/izzzzzi/agent-assh/internal/forward"
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

func newForwardCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "forward",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "forward subcommand required", "run assh forward --help")
		},
	}
	cmd.AddCommand(
		newForwardStartCommand(),
		newForwardStatusCommand(),
		newForwardStopCommand(),
	)
	return cmd
}

func newForwardStartCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var name string
	var persist time.Duration
	var local []string
	var remote []string
	var dynamic []string

	cmd := &cobra.Command{
		Use:           "start",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateForwardName(name); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if len(local)+len(remote)+len(dynamic) == 0 {
				return writeInvalidArgs(cmd, "at least one forwarding rule required", "")
			}
			if persist <= 0 {
				return writeInvalidArgs(cmd, "persist must be greater than 0", "")
			}

			socket := forward.ControlSocketPath(stateBaseDir(), name)
			if err := ensureForwardSocketDir(socket); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			spec := forwardSpecFromOptions(ssh, socket, persist, local, remote, dynamic)
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()
			result := runForward(ctx, forward.Command(spec), forward.StartArgs(spec))
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.Err != nil || result.ExitCode != 0 {
				return writeError(cmd, "forward_failed", sshResultErrorMessage(ctx.Err(), result), "")
			}

			record := forwardRecordFromSpec(name, spec)
			if err := state.NewForwardStore(stateBaseDir()).Save(record); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("forward_start", ssh.Host, ssh.User, "forward start", result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
			return writeJSON(cmd, forwardResponse(record, true, response.OK{"stopped": false}))
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&name, "name", "", "forward name")
	cmd.Flags().DurationVar(&persist, "persist", time.Hour, "OpenSSH ControlPersist duration")
	cmd.Flags().StringArrayVar(&local, "local-forward", nil, "local forwarding rule")
	cmd.Flags().StringArrayVar(&remote, "remote-forward", nil, "remote forwarding rule")
	cmd.Flags().StringArrayVar(&dynamic, "dynamic-forward", nil, "dynamic forwarding rule")
	return cmd
}

func newForwardStatusCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:           "status",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateForwardName(name); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			record, err := state.NewForwardStore(stateBaseDir()).Load(name)
			if err != nil {
				return writeError(cmd, "forward_not_found", err.Error(), "")
			}
			spec := specFromForwardRecord(record)
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(record.TimeoutSeconds)*time.Second)
			defer cancel()
			result := runForward(ctx, forward.Command(spec), forward.ControlArgs(spec, "check"))
			live := result.Err == nil && result.ExitCode == 0 && ctx.Err() == nil
			return writeJSON(cmd, forwardResponse(record, live, nil))
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "forward name")
	return cmd
}

func newForwardStopCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:           "stop",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateForwardName(name); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			store := state.NewForwardStore(stateBaseDir())
			record, err := store.Load(name)
			if err != nil {
				return writeError(cmd, "forward_not_found", err.Error(), "")
			}
			spec := specFromForwardRecord(record)
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(record.TimeoutSeconds)*time.Second)
			defer cancel()
			result := runForward(ctx, forward.Command(spec), forward.ControlArgs(spec, "exit"))
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.Err != nil || result.ExitCode != 0 {
				return writeError(cmd, "forward_failed", sshResultErrorMessage(ctx.Err(), result), "")
			}
			if err := store.Delete(name); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			writeAudit("forward_stop", record.Host, record.User, "forward stop", result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))
			return writeJSON(cmd, forwardResponse(record, false, response.OK{"stopped": true}))
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "forward name")
	return cmd
}

func validateForwardName(name string) error {
	if name == "" {
		return validationError("--name is required")
	}
	if !state.SafeForwardName(name) {
		return validationError("invalid forward name")
	}
	return nil
}

func ensureForwardSocketDir(socket string) error {
	return os.MkdirAll(filepath.Dir(socket), 0o700)
}

func forwardSpecFromOptions(ssh sshOptions, socket string, persist time.Duration, local []string, remote []string, dynamic []string) forward.Spec {
	return forward.Spec{
		Host:          ssh.Host,
		User:          ssh.User,
		Port:          ssh.Port,
		Identity:      ssh.Identity,
		Jump:          ssh.Jump,
		TimeoutSecond: ssh.TimeoutSecond,
		HostKeyPolicy: ssh.HostKeyPolicy,
		ControlSocket: socket,
		Persist:       persist,
		Local:         local,
		Remote:        remote,
		Dynamic:       dynamic,
	}
}

func forwardRecordFromSpec(name string, spec forward.Spec) state.ForwardRecord {
	return state.ForwardRecord{
		Name:             name,
		Host:             spec.Host,
		User:             spec.User,
		Port:             spec.Port,
		Identity:         spec.Identity,
		Jump:             spec.Jump,
		HostKeyPolicy:    spec.HostKeyPolicy,
		Local:            spec.Local,
		Remote:           spec.Remote,
		Dynamic:          spec.Dynamic,
		ControlSocket:    spec.ControlSocket,
		CreatedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
		PersistSeconds:   int64(spec.Persist.Seconds()),
		TimeoutSeconds:   spec.TimeoutSecond,
	}
}

func specFromForwardRecord(record state.ForwardRecord) forward.Spec {
	return forward.Spec{
		Host:          record.Host,
		User:          record.User,
		Port:          record.Port,
		Identity:      record.Identity,
		Jump:          record.Jump,
		TimeoutSecond: record.TimeoutSeconds,
		HostKeyPolicy: record.HostKeyPolicy,
		ControlSocket: record.ControlSocket,
		Persist:       time.Duration(record.PersistSeconds) * time.Second,
		Local:         record.Local,
		Remote:        record.Remote,
		Dynamic:       record.Dynamic,
	}
}

func forwardResponse(record state.ForwardRecord, live bool, extra response.OK) response.OK {
	body := response.OK{
		"ok":      true,
		"name":    record.Name,
		"host":    record.Host,
		"user":    record.User,
		"socket":  record.ControlSocket,
		"live":    live,
		"created": record.CreatedAtRFC3339,
		"rules": response.OK{
			"local":   record.Local,
			"remote":  record.Remote,
			"dynamic": record.Dynamic,
		},
	}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

var runForward = func(ctx context.Context, command transport.SSHCommand, args []string) transport.Result {
	binary := command.Binary
	if binary == "" {
		binary = "ssh"
	}
	execCommand := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	execCommand.Stdout = &stdout
	execCommand.Stderr = &stderr
	err := execCommand.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return transport.Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode, Err: err}
}
