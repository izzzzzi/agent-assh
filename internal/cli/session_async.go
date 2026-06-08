package cli

import (
	"context"
	"strconv"
	"time"

	"github.com/izzzzzi/agent-assh/internal/ids"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/safety"
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

			if result := safety.CheckCommand(userCommand); result.Dangerous {
				return writeError(cmd, "dangerous_command_requires_confirmation", "command looks destructive; exec-async does not support --confirm-danger for safety reasons", result.Message)
			}

			jobID, err := ids.New()
			if err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}
			jobName := "assh_job_" + jobID

			entry.Seq++
			remoteCommand := execAsyncRemoteCommand(entry.TmuxName, jobName, entry.Seq, userCommand, timeout)
			localTimeout := time.Duration(timeout+10) * time.Second
			ctx, cancel := context.WithTimeout(cmd.Context(), localTimeout)
			defer cancel()

			if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
				return writeError(cmd, "internal_error", err.Error(), "")
			}

			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, firstNonEmpty(ssh.Jump, entry.Jump), timeout+10, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
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
			if lines < 1 {
				lines = 50
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			jobName := "assh_job_" + jobID
			remoteCommand := jobStatusRemoteCommand(jobName, lines)
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, 30, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			alive := !containsStr(string(result.Stdout), "__JOB_NOT_FOUND__")
			completed := containsStr(string(result.Stdout), "__JOB_COMPLETE__")

			return writeJSON(cmd, map[string]any{
				"ok":        true,
				"sid":       sid,
				"job_id":    jobID,
				"alive":     alive,
				"completed": completed,
				"output":    string(result.Stdout),
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVar(&jobID, "job-id", "", "job id")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "output lines to return")
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

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			jobName := "assh_job_" + jobID
			remoteCommand := "tmux kill-window -t " + remote.SingleQuote(jobName) + " 2>/dev/null && echo '{\"ok\":true}' || echo '{\"ok\":false,\"error\":\"job not found\"}'"
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, 30, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			return writeJSON(cmd, map[string]any{
				"ok":     true,
				"sid":    sid,
				"job_id": jobID,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVar(&jobID, "job-id", "", "job id")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func execAsyncRemoteCommand(tmuxName, jobName string, seq int, command string, waitSeconds int) string {
	sessionDir := "~/.assh/sessions"
	jobDir := sessionDir + "/jobs/" + jobName
	outFile := jobDir + "/output.log"
	rcFile := jobDir + "/rc"
	cmd := "mkdir -p " + jobDir + " || exit $?; " +
		"tmux new-window -d -t " + remote.SingleQuote(tmuxName) + " -n " + remote.SingleQuote(jobName) + " '(" +
		command +
		") > " + outFile + " 2>&1; echo $? > " + rcFile + "; echo __JOB_COMPLETE__ >> " + outFile + "'"
	return cmd
}

func jobStatusRemoteCommand(jobName string, lines int) string {
	jobDir := "~/.assh/sessions/jobs/" + jobName
	outFile := jobDir + "/output.log"
	rcFile := jobDir + "/rc"
	return "test -f " + outFile + " || { echo __JOB_NOT_FOUND__; exit 0; }; " +
		"tail -n " + strconv.Itoa(lines) + " " + outFile + " 2>/dev/null; " +
		"test -f " + rcFile + " && printf '\\n__RC__=%s\\n' \"$(cat " + rcFile + ")\" || true"
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
