package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/ids"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/safety"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

// sessionExecOptions captures the cobra-bound flags for `session exec` so the
// RunE body can be extracted into runSessionExec and stay under funlen limit.
type sessionExecOptions struct {
	sid           string
	timeout       int
	confirmDanger bool
	stdin         bool
	beforeCmd     string
	afterCmd      string
}

const maxSessionStdinBytes int64 = 4 << 20

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
	cmd.Flags().BoolVar(&opts.stdin, "stdin", false,
		"forward piped stdin to the remote command via a temporary file")
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

	ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(opts.timeout+5)*time.Second)
	defer cancel()
	stdinPath, cleanup, err := prepareSessionStdin(ctx, cmd, opts, entry, ssh)
	if err != nil {
		return err
	}
	defer cleanup()

	// seq numbers attempts, not guaranteed outputs. It is bumped and persisted
	// before the command runs so a retry can never reuse a number and overwrite
	// an earlier output file on the remote. The trade-off is that a timed-out or
	// failed exec still consumes its number, leaving a gap and no readable
	// output for it — callers must read the seq returned by the exec that ran.
	entry, err = session.NextSeq(stateBaseDir(), entry.SID)
	if err != nil {
		return writeError(cmd, "internal_error", err.Error(), "")
	}
	remoteCommand := buildSessionExecRemote(entry, userCommand, opts.timeout, stdinPath)
	result := runSSH(ctx, sessionSSH(
		entry.Host, entry.User, entry.Port, entry.Identity,
		firstNonEmpty(ssh.Jump, entry.Jump),
		opts.timeout+5, entry.HostKeyPolicy, entry.ForcePTY,
	), remoteCommand)

	if strings.Contains(string(result.Stdout), "__ASSH_TIMEOUT__") {
		return writeError(cmd, "timeout", "session command timed out", sessionTimeoutHint(entry, entry.Seq))
	}
	if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
		if code == "timeout" {
			return writeError(cmd, "timeout", sshResultErrorMessage(ctx.Err(), result), sessionTimeoutHint(entry, entry.Seq))
		}
		return registeredSessionLifecycleError(cmd, entry, code, sshResultErrorMessage(ctx.Err(), result))
	}
	rc, stdoutLines, stderrLines, timedOut, known := parseSessionExec(result.Stdout)
	if timedOut {
		return writeError(cmd, "timeout", "session command timed out", sessionTimeoutHint(entry, entry.Seq))
	}
	if !known {
		hint := "do not blindly retry; run `assh session read -s " + entry.SID +
			" --seq " + strconv.Itoa(entry.Seq) + " --limit 50` and inspect the session"
		return writeError(cmd, "command_status_unknown", "session command result markers were not returned", hint)
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

// prepareSessionStdin stores piped stdin in a private local file and uploads it
// to the session directory. The payload never enters argv or the audit stream.
// The returned cleanup is best-effort for the remote file and deterministic for
// the local file; the deferred remote cleanup removes the uploaded file after
// both successful and failed command execution.
func prepareSessionStdin(ctx context.Context, cmd *cobra.Command, opts sessionExecOptions,
	entry session.RegistryEntry, ssh sshOptions) (string, func(), error) {
	if !opts.stdin {
		return "", func() {}, nil
	}
	if !stdinIsPiped() {
		return "", func() {}, writeInvalidArgs(cmd, "--stdin requires piped input", "")
	}

	file, err := os.CreateTemp("", "assh-stdin-*")
	if err != nil {
		return "", func() {}, writeError(cmd, "stdin_read_failed", err.Error(), "")
	}
	localPath := file.Name()
	_ = os.Chmod(localPath, 0o600)
	removeLocal := func() { _ = os.Remove(localPath) }

	limited := io.LimitReader(cmd.InOrStdin(), maxSessionStdinBytes+1)
	written, copyErr := io.Copy(file, limited)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		removeLocal()
		if copyErr != nil {
			return "", func() {}, writeError(cmd, "stdin_read_failed", copyErr.Error(), "")
		}
		return "", func() {}, writeError(cmd, "stdin_read_failed", closeErr.Error(), "")
	}
	if written > maxSessionStdinBytes {
		removeLocal()
		return "", func() {}, writeError(cmd, "stdin_too_large",
			fmt.Sprintf("stdin exceeds %d bytes", maxSessionStdinBytes),
			"use `assh transfer put` for larger files")
	}

	id, err := ids.New()
	if err != nil {
		removeLocal()
		return "", func() {}, writeError(cmd, "internal_error", err.Error(), "")
	}
	remotePath := "~/.assh/sessions/" + entry.SID + "/stdin-" + id
	removeRemote := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = runSSH(cleanupCtx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity,
			firstNonEmpty(ssh.Jump, entry.Jump), 30, entry.HostKeyPolicy, entry.ForcePTY),
			"rm -f "+shellRemotePath(remotePath))
	}
	upload := runSCP(ctx, scpCommandFromRegistryEntry(entry, firstNonEmpty(ssh.Jump, entry.Jump)),
		localPath, remotePath, transport.Upload)
	if code := sshResultErrorCode(ctx.Err(), upload); code != "" {
		removeLocal()
		removeRemote()
		return "", func() {}, writeError(cmd, code, sshResultErrorMessage(ctx.Err(), upload), "")
	}
	if upload.Err != nil || upload.ExitCode != 0 {
		removeLocal()
		removeRemote()
		return "", func() {}, writeError(cmd, "stdin_upload_failed",
			sshResultErrorMessage(ctx.Err(), upload), "")
	}

	cleanup := func() {
		removeLocal()
		removeRemote()
	}
	return remotePath, cleanup, nil
}

// shellRemotePath quotes a remote path while preserving a leading ~/ so the
// remote shell expands it. Quoting the tilde itself would make the shell look
// for a literal directory named `~`.
func shellRemotePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		return "~/" + remote.SingleQuote(path[2:])
	}
	return remote.SingleQuote(path)
}

func scpCommandFromRegistryEntry(entry session.RegistryEntry, jump string) transport.SCPCommand {
	return transport.SCPCommand{
		Host: entry.Host, User: entry.User, Port: entry.Port, Identity: entry.Identity,
		Jump: jump, TimeoutSecond: 30, HostKeyPolicy: entry.HostKeyPolicy, Batch: true,
	}
}

// validateSessionExecArgs checks sid/command/timeout presence and the stdin-pipe
// guard. Without --stdin, session exec still rejects piped input because tmux
// execution does not consume the caller's stdin. With --stdin, the explicit
// upload path handles the pipe instead.
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
	if stdinIsPiped() && !opts.stdin {
		return writeError(cmd, "stdin_pipe_unsupported",
			"session exec does not forward stdin without --stdin",
			"rerun with `--stdin`, or use `assh transfer put` for larger files")
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
func buildSessionExecRemote(entry session.RegistryEntry, userCommand string, timeout int, stdinPath string) string {
	if stdinPath != "" {
		// Group first: a bare `a | b < file` would redirect only b.
		userCommand = "{ " + userCommand + "; } < " + shellRemotePath(stdinPath)
	}
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

// rejectByProfile enforces the allow-list profile attached to a session at
// connect time. It runs on top of the safety classifier, never instead of it.
// A profile that cannot be loaded fails closed: a restricted session must not
// silently become unrestricted because profiles.json is unreadable.
func rejectByProfile(cmd *cobra.Command, profile, userCommand string) error {
	if profile == "" {
		return nil
	}
	const hint = "use a different profile or connect without --profile"
	profs, err := safety.LoadProfiles(safety.DefaultProfilePath())
	if err != nil || profs == nil {
		message := "profile '" + profile + "' could not be loaded"
		if err != nil {
			message += ": " + err.Error()
		}
		return writeError(cmd, "command_not_allowed", message, hint)
	}
	if r := profs.Match(profile, userCommand); r.Dangerous {
		return writeError(cmd, "command_not_allowed", r.Message, hint)
	}
	return nil
}

func parseSessionExec(stdout []byte) (int, int, int, bool, bool) {
	text := string(stdout)
	if strings.Contains(text, "__ASSH_TIMEOUT__") {
		return -1, 0, 0, true, false
	}
	if !strings.Contains(text, "__ASSH_RC__=") ||
		!strings.Contains(text, "__ASSH_STDOUT_LINES__=") ||
		!strings.Contains(text, "__ASSH_STDERR_LINES__=") {
		return -1, 0, 0, false, false
	}
	rc := markerInt(text, "__ASSH_RC__", 0)
	stdoutLines := markerInt(text, "__ASSH_STDOUT_LINES__", 0)
	stderrLines := markerInt(text, "__ASSH_STDERR_LINES__", 0)
	return rc, stdoutLines, stderrLines, false, true
}

func sessionTimeoutHint(entry session.RegistryEntry, seq int) string {
	return "the synchronous command exceeded its wait window; do not blindly retry; " +
		"for long commands use `assh session exec-async -s " + entry.SID + " -- \"cmd\"` then " +
		"`assh session job-status -s " + entry.SID + " --job-id JOB_ID`; inspect seq " + strconv.Itoa(seq) +
		" with `assh session read -s " + entry.SID + " --seq " + strconv.Itoa(seq) + " --limit 50`"
}
