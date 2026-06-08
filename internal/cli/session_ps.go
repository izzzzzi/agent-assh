package cli

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/spf13/cobra"
)

func newSessionProcessListCommand() *cobra.Command {
	var sid string
	var filter string
	var top int
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "ps",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if top < 1 {
				top = 20
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			remoteCommand := remoteProcessListCommand(filter, top)
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, 30, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			processes := parseProcessList(result.Stdout)
			return writeJSON(cmd, map[string]any{
				"ok":        true,
				"sid":       sid,
				"count":     len(processes),
				"top":       top,
				"filter":    filter,
				"processes": processes,
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVarP(&filter, "filter", "f", "", "filter by process name")
	cmd.Flags().IntVarP(&top, "top", "n", 20, "number of top processes (by CPU)")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func newSessionProcessKillCommand() *cobra.Command {
	var sid string
	var pid int
	var signal string
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "kill",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if pid < 1 {
				return writeInvalidArgs(cmd, "--pid is required", "")
			}
			if pid == 1 {
				return writeError(cmd, "dangerous_command_requires_confirmation", "killing PID 1 (init/systemd) is blocked for safety", "")
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			sig := signal
			if sig == "" {
				sig = "TERM"
			}

			remoteCommand := "kill -" + sig + " " + strconv.Itoa(pid) + " 2>/dev/null && echo '{\"ok\":true,\"pid\":'" + strconv.Itoa(pid) + ",\"signal\":\"" + sig + "\"}' || echo '{\"ok\":false,\"pid\":'" + strconv.Itoa(pid) + ",\"error\":\"kill failed\"}'"
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, 30, entry.HostKeyPolicy, entry.ForcePTY), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			var killResult map[string]any
			if err := json.Unmarshal(result.Stdout, &killResult); err != nil {
				killResult = map[string]any{"ok": false, "error": "failed to parse kill result"}
			}
			killResult["sid"] = sid
			return writeJSON(cmd, killResult)
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().IntVarP(&pid, "pid", "p", 0, "process id to kill")
	cmd.Flags().StringVar(&signal, "signal", "TERM", "signal name (TERM, KILL, HUP, INT)")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func remoteProcessListCommand(filter string, top int) string {
	cmd := `ps -eo pid,ppid,user,rss,vsz,%cpu,%mem,etime,args --sort=-%cpu 2>/dev/null | head -n $((` + strconv.Itoa(top) + ` + 1))`
	if filter != "" {
		cmd += ` | grep -i '` + filter + `' || true`
	}
	cmd += ` | awk 'NR==1{printf "["} NR>1{printf "%s{\"pid\":%s,\"ppid\":%s,\"user\":\"%s\",\"rss_kb\":%s,\"vsz_kb\":%s,\"cpu_pct\":%s,\"mem_pct\":%s,\"etime\":\"%s\",\"command\":\"%s\"}",(NR>2?",":""),$1,$2,$3,$4,$5,$6,$7,$8,substr($0,index($0,$9))} END{print "]"}'`
	return cmd
}

func parseProcessList(stdout []byte) []any {
	text := string(stdout)
	if text == "" || text == "[]" {
		return make([]any, 0)
	}
	var processes []any
	if err := json.Unmarshal([]byte(text), &processes); err != nil {
		return make([]any, 0)
	}
	return processes
}
