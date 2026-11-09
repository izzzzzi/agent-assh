package cli

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"time"

	"github.com/izzzzzi/agent-assh/internal/redact"
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
				return writeError(cmd, "dangerous_command_requires_confirmation",
					"command looks destructive: "+result.Message,
					"fleet exec does not support --confirm-danger for safety reasons")
			}

			results := runFleetExec(hosts, ssh, command, timeout)
			failed := 0
			for _, result := range results {
				if result["ok"] != true {
					failed++
				}
			}

			// Top-level ok reports that the fan-out ran; per-host outcomes are in
			// results, and failed counts the hosts that did not succeed.
			return writeJSON(cmd, map[string]any{
				"ok":      true,
				"command": command,
				"hosts":   len(hosts),
				"failed":  failed,
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

// fleetInlineLimit caps each host's stdout and stderr in the response. fleet
// returns output inline rather than through the output store, so without a cap
// N hosts times a large output would land in the caller's context at once.
const fleetInlineLimit = 8 << 10

func runFleetExec(hosts []string, baseOpts sshOptions, command string, timeout int) []map[string]any {
	var wg sync.WaitGroup
	// Indexed by input position: results come back in the order hosts were given.
	results := make([]map[string]any, len(hosts))

	const maxConcurrent = 50
	sem := make(chan struct{}, maxConcurrent)

	parentCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+5)*time.Second)
	defer cancel()

	for i, host := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, h string) {
			defer wg.Done()
			defer func() { <-sem }()
			opts := baseOpts
			opts.Host = h
			opts.TimeoutSecond = timeout

			ctx, cancel := context.WithTimeout(parentCtx, time.Duration(timeout)*time.Second)
			defer cancel()

			result := runSSH(ctx, opts.command(), command)

			stdout, outRes := redact.Bytes(result.Stdout)
			stderr, errRes := redact.Bytes(result.Stderr)
			stdoutText, outCut := tailBytes(stdout, fleetInlineLimit)
			stderrText, errCut := tailBytes(stderr, fleetInlineLimit)
			entry := map[string]any{
				"host":      h,
				"ok":        result.Err == nil && result.ExitCode == 0,
				"exit_code": result.ExitCode,
				"stdout":    stdoutText,
				"stderr":    stderrText,
			}
			if outRes.Count+errRes.Count > 0 {
				entry["redacted"] = true
			}
			if outCut || errCut {
				entry["truncated"] = true
			}
			if result.Err != nil {
				entry["error"] = result.Err.Error()
			}
			results[i] = entry
		}(i, host)
	}

	wg.Wait()
	return results
}

// tailBytes keeps the last limit bytes of data, cutting at a line boundary when
// one is available. The tail is the part that carries the error or the result.
func tailBytes(data []byte, limit int) (string, bool) {
	if len(data) <= limit {
		return string(data), false
	}
	tail := data[len(data)-limit:]
	if i := bytes.IndexByte(tail, '\n'); i >= 0 && i+1 < len(tail) {
		tail = tail[i+1:]
	}
	return string(tail), true
}
