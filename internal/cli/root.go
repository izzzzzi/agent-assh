package cli

import (
	"errors"
	"strings"

	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "assh",
		Short:         "SSH workflow helper for LLM agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unknown command "+args[0],
					unknownCommandHint(cmd.Root(), args[0]))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "command required", "run assh --help")
		},
	}
	// Accepted no-op, kept for backwards compatibility: scripts written against
	// earlier versions pass --json explicitly, and operational commands always
	// emit JSON now. The contract is pinned by
	// TestDeprecatedJSONFlagRemainsAccepted — do not drop the flag without
	// deciding to break those callers.
	cmd.PersistentFlags().Bool("json", true,
		"emit JSON output (deprecated; JSON is always emitted for operational commands)")
	_ = cmd.PersistentFlags().MarkHidden("json")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return writeInvalidArgs(cmd, err.Error(), flagErrorHint(err))
	})
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		writeAgentHelp(cmd.Root())
	})
	cmd.AddCommand(
		newSessionListRootAlias(),
		newPromptCommand(),
		newConnectCommand(),
		newConnectInfoCommand(),
		newExecCommand(),
		newReadCommand(),
		newCapabilitiesCommand(),
		newSessionCommand(),
		newTransferCommand(),
		newForwardCommand(),
		newScanCommand(),
		newKeyDeployCommand(),
		newAuditCommand(),
		newVersionCommand(),
		newFleetExecCommand(),
	)
	setCommandHelp(cmd)
	return cmd
}

// flagErrorHint adds guidance for flags that exist elsewhere in the CLI.
// `-E`/`--password-env` is the common case: it is registered only on the
// first-login commands, so `assh exec -E …` fails with a bare "unknown
// shorthand flag" that reads like a typo. Callers then retry the same command
// instead of bootstrapping once, so the hint spells out the working path.
func flagErrorHint(err error) string {
	message := err.Error()
	if strings.Contains(message, "password-env") || strings.Contains(message, "'E' in -E") {
		return "-E/--password-env is accepted only by the first-login commands " +
			"(`assh connect`, `assh key-deploy`); `exec` and " +
			"`transfer` authenticate with `-i KEY`. Bootstrap once with " +
			"`assh connect -H HOST -u USER -E PASS_ENV -n NAME`, then run the command " +
			"against the session: `assh session exec -s SID -- \"cmd\"`"
	}
	return "run assh --help for the full list"
}

// unknownCommandHint suggests the closest command name for a typo. Cobra's own
// suggestion machinery is bypassed because this CLI renders errors as JSON.
func unknownCommandHint(root *cobra.Command, name string) string {
	best, bestDistance := "", 0
	for _, child := range root.Commands() {
		for _, candidate := range append([]string{child.Name()}, child.Aliases...) {
			if distance := levenshtein(name, candidate); best == "" || distance < bestDistance {
				best, bestDistance = candidate, distance
			}
		}
	}
	// Only suggest a near miss: a distant name would point somewhere arbitrary.
	if best != "" && bestDistance <= 2 {
		return "did you mean `assh " + best + "`? run assh --help for the full list"
	}
	return "run assh --help for the full list"
}

// levenshtein returns the edit distance between two command names. It is used
// only for "did you mean" suggestions, so the plain O(len(a)*len(b)) table is
// far more than fast enough and keeps the implementation obvious.
func levenshtein(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			substitution := previous[j-1]
			if a[i-1] != b[j-1] {
				substitution++
			}
			current[j] = min(previous[j]+1, current[j-1]+1, substitution)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func Execute() error {
	return NewRootCommand().Execute()
}

func noPositionalArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return writeInvalidArgs(cmd, "unexpected positional arguments", "")
	}
	return nil
}

func writeInvalidArgs(cmd *cobra.Command, message, hint string) error {
	body, marshalErr := response.MarshalError("invalid_args", message, hint)
	if marshalErr != nil {
		return marshalErr
	}
	_, _ = cmd.ErrOrStderr().Write(body)
	return errors.New(message)
}

func writeJSON(cmd *cobra.Command, v any) error {
	body, err := response.Marshal(v)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(body)
	return err
}

func writeCommandHelp(cmd *cobra.Command, args []string) {
	if _, err := cmd.OutOrStdout().Write([]byte(cmd.UsageString())); err != nil {
		_, _ = cmd.ErrOrStderr().Write([]byte("failed to write help: " + err.Error() + "\n"))
	}
}

func setCommandHelp(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		child.SetHelpFunc(writeCommandHelp)
		setCommandHelp(child)
	}
}

func writeError(cmd *cobra.Command, code, message, hint string) error {
	body, marshalErr := response.MarshalError(code, message, hint)
	if marshalErr != nil {
		return marshalErr
	}
	_, _ = cmd.ErrOrStderr().Write(body)
	return errors.New(message)
}
