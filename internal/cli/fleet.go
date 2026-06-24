package cli

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

func newFleetExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "fleet",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "fleet subcommand required", "run assh fleet --help")
		},
	}
	cmd.AddCommand(
		newFleetExecRunCommand(),
	)
	return cmd
}

func newFleetExecRunCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var hosts []string
	var timeout int
	var user string

	cmd := &cobra.Command{
		Use:           "exec -- command...",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(hosts) == 0 {
				return writeInvalidArgs(cmd, "--hosts is required", "")
			}
			if len(args) == 0 {
				return writeInvalidArgs(cmd, "command required", "")
			}
			if timeout < 1 {
				timeout = 60
			}

			if user != "" {
				ssh.User = user
			}
			command := strings.Join(args, " ")

			if result, handled, errReturn := classifyCommand(cmd, command); handled {
				return errReturn
			} else if result.Dangerous {
				return writeError(cmd, "dangerous_command_requires_confirmation", "command looks destructive: "+result.Message, "fleet exec does not support --confirm-danger for safety reasons")
			}

			results := runFleetExec(hosts, ssh, command, timeout)

			return writeJSON(cmd, map[string]any{
				"ok":      true,
				"command": command,
				"hosts":   len(hosts),
				"results": results,
			})
		},
	}

	cmd.Flags().StringArrayVarP(&hosts, "hosts", "H", nil, "target hosts (repeatable)")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 60, "timeout in seconds")
	cmd.Flags().StringVarP(&user, "user", "u", "", "SSH user for all hosts")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{identity: true, jump: true, hostKeyPolicy: true})
	return cmd
}

func runFleetExec(hosts []string, baseOpts sshOptions, command string, timeout int) []map[string]any {
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := make([]map[string]any, 0, len(hosts))

	const maxConcurrent = 50
	sem := make(chan struct{}, maxConcurrent)

	parentCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+5)*time.Second)
	defer cancel()

	for _, host := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()
			opts := baseOpts
			opts.Host = h
			opts.TimeoutSecond = timeout

			ctx, cancel := context.WithTimeout(parentCtx, time.Duration(timeout)*time.Second)
			defer cancel()

			result := runSSH(ctx, opts.command(), command)

			entry := map[string]any{
				"host":      h,
				"ok":        result.Err == nil && result.ExitCode == 0,
				"exit_code": result.ExitCode,
				"stdout":    string(result.Stdout),
				"stderr":    string(result.Stderr),
			}
			if result.Err != nil {
				entry["error"] = result.Err.Error()
			}

			mu.Lock()
			results = append(results, entry)
			mu.Unlock()
		}(host)
	}

	wg.Wait()
	return results
}
