package cli

import (
	"context"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/safety"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/spf13/cobra"
)

// sessionExecOptions captures the cobra-bound flags for `session exec` so the
// RunE body can be extracted into runSessionExec and stay under funlen limit.
type sessionExecOptions struct {
	sid           string
	timeout       int
	confirmDanger bool
	beforeCmd     string
	afterCmd      string
}

func newSessionExecCommand() *cobra.Command {
	opts := sessionExecOptions{}
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "exec -- command",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionExec(cmd, args, opts, ssh)
		},
	}

	cmd.Flags().StringVarP(&opts.sid, "sid", "s", "", "session id")
	cmd.Flags().IntVarP(&opts.timeout, "timeout", "t", 300, "timeout in seconds")
	cmd.Flags().BoolVar(&opts.confirmDanger, "confirm-danger", false,
		"allow a command that matches destructive safety rules")
	cmd.Flags().StringVar(&opts.beforeCmd, "before", "", "shell command to run before the main command")
	cmd.Flags().StringVar(&opts.afterCmd, "after", "", "shell command to run after the main command")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func runSessionExec(cmd *cobra.Command, args []string, opts sessionExecOptions, ssh sshOptions) error {
	if err := validateSessionExecArgs(cmd, opts, args); err != nil {
		return err
	}

	entry, err := session.LoadRegistry(stateBaseDir(), opts.sid)
	if err != nil {
		return writeError(cmd, "session_not_found", err.Error(), "")
	}

	userCommand := remoteCommand(args)
	userCommand = wrapBeforeAfter(userCommand, opts.beforeCmd, opts.afterCmd)
	if result, handled, errReturn := classifyCommand(cmd, userCommand); handled {
		return errReturn
	} else if result.Dangerous && !opts.confirmDanger {
		return writeError(cmd, "dangerous_command_requires_confirmation",
			"command looks destructive; rerun with --confirm-danger if intentional",
			result.Message)
	}
	if err := rejectByProfile(cmd, entry.Profile, userCommand); err != nil {
		return err
	}

	entry.Seq++
	remoteCommand := buildSessionExecRemote(entry, userCommand, opts.timeout)

	ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(opts.timeout+5)*time.Second)
	defer cancel()
	if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
		return writeError(cmd, "internal_error", err.Error(), "")
	}
	result := runSSH(ctx, sessionSSH(
		entry.Host, entry.User, entry.Port, entry.Identity,
		firstNonEmpty(ssh.Jump, entry.Jump),
		opts.timeout+5, entry.HostKeyPolicy, entry.ForcePTY,
	), remoteCommand)

	if strings.Contains(string(result.Stdout), "__ASSH_TIMEOUT__") {
		return writeError(cmd, "timeout", "session command timed out", "")
	}
	if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
		return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
	}
	rc, stdoutLines, stderrLines, timedOut := parseSessionExec(result.Stdout)
	if timedOut {
		return writeError(cmd, "timeout", "session command timed out", "")
	}
	writeAudit("session_exec", entry.SID, entry.Host, entry.User, remoteCommand, rc, stdoutLines, stderrLines)
	_ = state.NewTranscriptStore(stateBaseDir()).Append(entry.SID, entry.Seq, userCommand, nil, nil)

	return writeJSON(cmd, response.OK{
		"ok":           true,
		"rc":           rc,
		"seq":          entry.Seq,
		"stdout_lines": stdoutLines,
		"stderr_lines": stderrLines,
		"sid":          opts.sid,
		"session":      entry.Label,
	})
}

// validateSessionExecArgs checks sid/command/timeout presence and the stdin-pipe
// guard. session exec forwards the command to a remote tmux window over
// non-interactive ssh and never forwards local stdin, so a pipe like
// `cat f | assh session exec ... cat >f` would silently drop the data and
// wedge the tmux window. We refuse it up front with a typed error.
func validateSessionExecArgs(cmd *cobra.Command, opts sessionExecOptions, args []string) error {
	if !remote.SafeSID(opts.sid) {
		return writeInvalidArgs(cmd, "--sid is required", "")
	}
	if len(args) == 0 {
		return writeInvalidArgs(cmd, "command required", "")
	}
	if opts.timeout < 1 {
		return writeInvalidArgs(cmd, "timeout must be greater than 0", "")
	}
	if stdinIsPiped() {
		return writeError(cmd, "stdin_pipe_unsupported",
			"session exec does not forward stdin to the remote command",
			"use `assh transfer put` to upload a file, or write the data to a "+
				"file locally and run `assh session exec` against it")
	}
	return nil
}

// wrapBeforeAfter wraps a user command in an optional before/after shell
// prefix/suffix: `{ before; user; after; }`. Empty before/after collapse away.
func wrapBeforeAfter(userCommand, beforeCmd, afterCmd string) string {
	if beforeCmd == "" && afterCmd == "" {
		return userCommand
	}
	wrapped := "{ "
	if beforeCmd != "" {
		wrapped += beforeCmd + "; "
	}
	wrapped += userCommand
	if afterCmd != "" {
		wrapped += "; " + afterCmd
	}
	return wrapped + "; }"
}

// buildSessionExecRemote builds the remote shell command for `session exec`.
// PTY-gated hosts (RunPod) need a temp script + tmux new-window because
// tmux send-keys breaks under PTY echo.
func buildSessionExecRemote(entry session.RegistryEntry, userCommand string, timeout int) string {
	if entry.ForcePTY {
		return execSyncRemoteCommandPTY(entry.SID, entry.TmuxName, entry.Seq, userCommand, timeout)
	}
	remoteCommand, err := session.ExecRemoteCommand(entry.SID, entry.TmuxName, entry.Seq, userCommand, timeout)
	if err != nil {
		// ExecRemoteCommand only errors on invalid sid/seq/pagination, all of
		// which are validated above; treat a late error as internal.
		return ""
	}
	return remoteCommand
}

// rejectByProfile enforces an additive deny profile attached to a session at
// connect time. It never replaces the safety classifier, only adds rules.
func rejectByProfile(cmd *cobra.Command, profile, userCommand string) error {
	if profile == "" {
		return nil
	}
	profs, err := safety.LoadProfiles(safety.DefaultProfilePath())
	if err != nil || profs == nil {
		return nil
	}
	if r := profs.Match(profile, userCommand); r.Dangerous {
		return writeError(cmd, "command_not_allowed", r.Message,
			"use a different profile or connect without --profile")
	}
	return nil
}

func parseSessionExec(stdout []byte) (int, int, int, bool) {
	text := string(stdout)
	if strings.Contains(text, "__ASSH_TIMEOUT__") {
		return -1, 0, 0, true
	}
	rc := markerInt(text, "__ASSH_RC__", 0)
	stdoutLines := markerInt(text, "__ASSH_STDOUT_LINES__", 0)
	stderrLines := markerInt(text, "__ASSH_STDERR_LINES__", 0)
	return rc, stdoutLines, stderrLines, false
}
