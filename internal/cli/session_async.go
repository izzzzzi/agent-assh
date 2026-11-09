package cli

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/ids"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/spf13/cobra"
)

func newSessionExecAsyncCommand() *cobra.Command {
	var sid string
	var timeout int
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "exec-async",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if len(args) == 0 {
				return writeInvalidArgs(cmd, "command required", "")
			}
			if timeout < 1 {
				timeout = 3600
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			userCommand := remoteCommand(args)

			if result, handled, errReturn := classifyCommand(cmd, userCommand); handled {
				return errReturn
			} else if result.Dangerous {
				return writeError(cmd, "dangerous_command_requires_confirmation",
					"command looks destructive; exec-async does not support --confirm-danger for safety reasons",
					result.Message)
			}
			if err := rejectByProfile(cmd, entry.Profile, userCommand); err != nil {
				return err
			}

			jobID, err := ids.New()
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			jobName := "assh_job_" + jobID

			entry, err = session.NextSeq(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			remoteCommand := execAsyncRemoteCommand(sid, entry.TmuxName, jobName, userCommand)
			localTimeout := time.Duration(timeout+10) * time.Second
			ctx, cancel := context.WithTimeout(cmd.Context(), localTimeout)
			defer cancel()

			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity,
				firstNonEmpty(ssh.Jump, entry.Jump), timeout+10, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				if code == "timeout" {
					return writeError(cmd, "timeout", sshResultErrorMessage(ctx.Err(), result),
						"async launch timed out; do not assume the job started; inspect `assh session job-status` "+
							"only if a job id was returned")
				}
				return registeredSessionLifecycleError(cmd, entry, code, sshResultErrorMessage(ctx.Err(), result))
			}

			return writeJSON(cmd, map[string]any{
				"ok":         true,
				"sid":        sid,
				"job_id":     jobID,
				"job_name":   jobName,
				"seq":        entry.Seq,
				"session":    entry.Label,
				"status_cmd": "assh session job-status -s " + sid + " --job-id " + jobID,
				"cancel_cmd": "assh session job-cancel -s " + sid + " --job-id " + jobID,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 3600, "timeout in seconds")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func newSessionJobStatusCommand() *cobra.Command {
	var sid string
	var jobID string
	var lines int
	var raw bool
	var noRedact bool
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "job-status",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if jobID == "" {
				return writeInvalidArgs(cmd, "--job-id is required", "")
			}
			// jobID flows into a remote `mkdir -p .../<jobName>` path and a
			// tmux window name with NO shell quoting. Reject anything that
			// is not a valid assh id, otherwise a caller-supplied value like
			// "x; <cmd>" executes arbitrary commands on the remote host.
			if !ids.Valid(jobID) {
				return writeInvalidArgs(cmd, "--job-id must be a valid assh job id (hex, 8-32 chars)", "")
			}
			if lines < 1 {
				lines = 50
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			jobName := "assh_job_" + jobID
			remoteCommand := jobStatusRemoteCommand(sid, jobName, lines)
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump,
				30, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			job := parseJobState(result.Stdout)
			output, redactionCount := applyReadRedaction(job.output, noRedact)
			if raw {
				_, _ = cmd.OutOrStdout().Write([]byte(output))
				return nil
			}

			payload := map[string]any{
				"ok":              true,
				"sid":             sid,
				"job_id":          jobID,
				"status":          job.status,
				"alive":           job.status == "running",
				"completed":       job.status == "completed",
				"output":          output,
				"redacted":        redactionCount > 0,
				"redaction_count": redactionCount,
			}
			if job.status == "completed" {
				payload["rc"] = job.rc
			}
			return writeJSON(cmd, payload)
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVar(&jobID, "job-id", "", "job id")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "output lines to return")
	cmd.Flags().BoolVar(&raw, "raw", false, "print only content without JSON")
	cmd.Flags().BoolVar(&noRedact, "no-redact", false, "disable best-effort secret redaction in served output")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func newSessionJobCancelCommand() *cobra.Command {
	var sid string
	var jobID string
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "job-cancel",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if jobID == "" {
				return writeInvalidArgs(cmd, "--job-id is required", "")
			}
			// See job-status: jobID is used unquoted in remote paths and
			// tmux targets; reject non-assh-id values to prevent command
			// injection via --job-id.
			if !ids.Valid(jobID) {
				return writeInvalidArgs(cmd, "--job-id must be a valid assh job id (hex, 8-32 chars)", "")
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			remoteCommand := jobCancelRemoteCommand(sid, entry.TmuxName, "assh_job_"+jobID)
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump,
				30, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			job := parseJobState(result.Stdout)
			if job.status == "not_found" {
				return writeError(cmd, "job_not_found", "no such job in session "+sid,
					"use the job_id returned by `assh session exec-async`")
			}
			return writeJSON(cmd, map[string]any{
				"ok":     true,
				"sid":    sid,
				"job_id": jobID,
				"status": job.status,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVar(&jobID, "job-id", "", "job id")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

// jobDir keeps a job's files inside its session directory, so closing or
// collecting the session removes them along with everything else.
func jobDir(sid, jobName string) string {
	return "~/.assh/sessions/" + sid + "/jobs/" + jobName
}

const jobStateMarker = "__ASSH_JOB_STATE__="

func execAsyncRemoteCommand(sid, tmuxName, jobName, command string) string {
	dir := jobDir(sid, jobName)
	outFile := dir + "/output.log"
	rcFile := dir + "/rc"
	scriptFile := dir + "/cmd.sh"
	return strings.Join([]string{
		"mkdir -p " + dir + "/",
		"printf '%s\\n' " + remote.SingleQuote(command) + " > " + scriptFile,
		"tmux new-window -d -t " + remote.SingleQuote(tmuxName) + " -n " + remote.SingleQuote(jobName) +
			" sh -c 'if command -v bash >/dev/null 2>&1; then bash " + scriptFile + "; else sh " + scriptFile +
			"; fi > " + outFile + " 2>&1; echo $? > " + rcFile + "'",
	}, " && ")
}

// jobStateSnippet prints the job state as the last line of stdout. A cancelled
// marker wins over rc: the killed runner may still get to write its exit status.
func jobStateSnippet(dir string) string {
	return "if test -f " + dir + "/cancelled; then s=cancelled; " +
		"elif test -s " + dir + "/rc; then s=\"rc:$(cat " + dir + "/rc)\"; else s=running; fi; " +
		"printf '\\n" + jobStateMarker + "%s\\n' \"$s\""
}

func jobStatusRemoteCommand(sid, jobName string, lines int) string {
	dir := jobDir(sid, jobName)
	return "test -f " + dir + "/cmd.sh || { echo " + jobStateMarker + "not_found; exit 0; }; " +
		"tail -n " + strconv.Itoa(lines) + " " + dir + "/output.log 2>/dev/null; " +
		jobStateSnippet(dir)
}

func jobCancelRemoteCommand(sid, tmuxName, jobName string) string {
	dir := jobDir(sid, jobName)
	return "test -f " + dir + "/cmd.sh || { echo " + jobStateMarker + "not_found; exit 0; }; " +
		"test -s " + dir + "/rc || { tmux kill-window -t " + remote.SingleQuote(tmuxName+":"+jobName) +
		" 2>/dev/null; : > " + dir + "/cancelled; }; " +
		jobStateSnippet(dir)
}

type jobState struct {
	status string // running | completed | cancelled | not_found
	rc     int
	output string
}

// parseJobState splits job-status stdout into the job output and the state
// line appended by jobStateSnippet.
func parseJobState(stdout []byte) jobState {
	text := string(stdout)
	idx := strings.LastIndex(text, jobStateMarker)
	if idx < 0 {
		return jobState{status: "running", output: text}
	}
	state := strings.TrimSpace(text[idx+len(jobStateMarker):])
	job := jobState{status: state, output: strings.TrimSuffix(text[:idx], "\n")}
	if value, ok := strings.CutPrefix(state, "rc:"); ok {
		job.status = "completed"
		job.rc, _ = strconv.Atoi(strings.TrimSpace(value))
	}
	return job
}

// execSyncRemoteCommandPTY runs a command synchronously using temp script + tmux new-window.
// Used instead of ExecRemoteCommand when ForcePTY is active (PTY-gated hosts like RunPod).
// The runner merges stderr into stdout, so the stderr stream is always empty.
func execSyncRemoteCommandPTY(sid, tmuxName string, seq int, command string, waitSeconds int) string {
	sessionDir := "~/.assh/sessions/" + sid
	dir := jobDir(sid, "exec_"+strconv.Itoa(seq))
	outFile := dir + "/output.log"
	rcFile := dir + "/rc"
	scriptFile := dir + "/cmd.sh"
	sessionOut := sessionDir + "/" + strconv.Itoa(seq) + ".out"
	sessionErr := sessionDir + "/" + strconv.Itoa(seq) + ".err"
	sessionRC := sessionDir + "/" + strconv.Itoa(seq) + ".rc"
	lineCount := "awk 'END{print NR}'"
	return strings.Join([]string{
		"mkdir -p " + dir + "/",
		"printf '%s\\n' " + remote.SingleQuote(command) + " > " + scriptFile,
		"rm -f " + rcFile,
		"tmux new-window -d -t " + remote.SingleQuote(tmuxName) +
			" -n exec sh -c 'if command -v bash >/dev/null 2>&1; then bash " +
			scriptFile + "; else sh " + scriptFile + "; fi > " + outFile + " 2>&1; echo $? > " + rcFile + "'",
		"i=0; while [ $i -lt " + strconv.Itoa(waitSeconds) + " ] && [ ! -f " + rcFile + " ]; do i=$((i+1)); sleep 1; done",
		"test -f " + rcFile + " || { echo __ASSH_TIMEOUT__; exit 124; }",
		"cp " + outFile + " " + sessionOut + " && : > " + sessionErr + " && cp " + rcFile + " " + sessionRC,
		"printf '__ASSH_RC__=%s\\n' \"$(cat " + rcFile + ")\"",
		"printf '__ASSH_STDOUT_LINES__=%s\\n' \"$(" + lineCount + " " + outFile + ")\"",
		"printf '__ASSH_STDERR_LINES__=0\\n'",
	}, " && ")
}
