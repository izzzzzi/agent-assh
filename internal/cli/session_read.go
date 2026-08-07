package cli

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/redact"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/spf13/cobra"
)

// sessionReadOptions captures cobra-bound flags for `session read` so the
// RunE body stays under the funlen limit.
type sessionReadOptions struct {
	sid      string
	seq      int
	stream   string
	offset   int
	limit    int
	raw      bool
	noRedact bool
}

func newSessionReadCommand() *cobra.Command {
	opts := sessionReadOptions{stream: "stdout", offset: 0, limit: 50}
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "read",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionRead(cmd, opts, ssh)
		},
	}

	cmd.Flags().StringVarP(&opts.sid, "sid", "s", "", "session id")
	cmd.Flags().IntVar(&opts.seq, "seq", 0, "session command sequence")
	cmd.Flags().StringVar(&opts.stream, "stream", "stdout", "stdout|stderr")
	cmd.Flags().IntVar(&opts.offset, "offset", 0, "line offset")
	cmd.Flags().IntVar(&opts.limit, "limit", 50, "line limit")
	cmd.Flags().BoolVar(&opts.raw, "raw", false, "print only content without JSON")
	cmd.Flags().BoolVar(&opts.noRedact, "no-redact", false, "disable best-effort secret redaction in served output")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func runSessionRead(cmd *cobra.Command, opts sessionReadOptions, ssh sshOptions) error {
	if err := validateSessionReadArgs(cmd, opts); err != nil {
		return err
	}

	entry, err := session.LoadRegistry(stateBaseDir(), opts.sid)
	if err != nil {
		return writeError(cmd, "session_not_found", err.Error(), "")
	}
	remoteCommand, err := session.ReadRemoteCommand(entry.SID, opts.seq, opts.stream, opts.offset, opts.limit)
	if err != nil {
		return writeInvalidArgs(cmd, err.Error(), "")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 300*time.Second)
	defer cancel()
	result := runSSH(ctx, sessionSSH(
		entry.Host, entry.User, entry.Port, entry.Identity,
		firstNonEmpty(ssh.Jump, entry.Jump),
		300, entry.HostKeyPolicy, entry.ForcePTY,
	), remoteCommand)
	if code := lifecycleResultErrorCode(ctx.Err(), result); code != "" {
		return registeredSessionLifecycleError(cmd, entry, code, sshResultErrorMessage(ctx.Err(), result))
	}

	content, total, notFound := parseSessionRead(result.Stdout)
	if notFound {
		return writeError(cmd, "output_not_found", "session output not found", reconnectHint(entry))
	}
	content, redactionCount := applyReadRedaction(content, opts.noRedact)
	if err := persistSessionReadOutput(opts, content, total); err != nil {
		return writeError(cmd, "internal_error", err.Error(), "")
	}
	if opts.stream == "stdout" {
		_ = state.NewTranscriptStore(stateBaseDir()).Append(opts.sid, opts.seq, "", []byte(content), nil)
	}
	if opts.raw {
		_, err := cmd.OutOrStdout().Write([]byte(content))
		return err
	}

	hasMore := opts.offset+opts.limit < total
	writeAudit("session_read", entry.SID, entry.Host, entry.User, remoteCommand, result.ExitCode,
		countLines(result.Stdout), countLines(result.Stderr))
	writeAuditSavings("session_read", entry.SID, total, countLines([]byte(content)))

	return writeJSON(cmd, response.OK{
		"ok":              true,
		"sid":             opts.sid,
		"seq":             opts.seq,
		"stream":          opts.stream,
		"offset":          opts.offset,
		"limit":           opts.limit,
		"total_lines":     total,
		"has_more":        hasMore,
		"content":         content,
		"redacted":        redactionCount > 0,
		"redaction_count": redactionCount,
	})
}

func validateSessionReadArgs(cmd *cobra.Command, opts sessionReadOptions) error {
	if !remote.SafeSID(opts.sid) {
		return writeInvalidArgs(cmd, "--sid is required", "")
	}
	if opts.seq < 1 {
		return writeInvalidArgs(cmd, "--seq is required", "")
	}
	if opts.stream != "stdout" && opts.stream != "stderr" {
		return writeInvalidArgs(cmd, "invalid stream", "")
	}
	if opts.offset < 0 || opts.limit < 1 {
		return writeInvalidArgs(cmd, "invalid pagination", "")
	}
	return nil
}

// applyReadRedaction masks known secret patterns inline. Redaction is a
// best-effort hygiene filter, NOT a security boundary; the secret already
// transited the transport. Returns the (possibly modified) content and the
// number of substitutions made.
func applyReadRedaction(content string, noRedact bool) (string, int) {
	if noRedact {
		return content, 0
	}
	res, redRes := redact.String(content)
	return res, redRes.Count
}

func persistSessionReadOutput(opts sessionReadOptions, content string, total int) error {
	return state.NewSessionOutputStore(stateBaseDir()).Write(state.SessionOutputPage{
		SID:        opts.sid,
		Seq:        opts.seq,
		Stream:     opts.stream,
		Offset:     opts.offset,
		Limit:      opts.limit,
		TotalLines: total,
		Content:    content,
	})
}

func parseSessionRead(stdout []byte) (string, int, bool) {
	text := string(stdout)
	if strings.TrimSpace(text) == "__ASSH_NOT_FOUND__" {
		return "", 0, true
	}
	marker := "\n__ASSH_TOTAL_LINES__="
	idx := strings.LastIndex(text, marker)
	if idx == -1 {
		return text, countLines(stdout), false
	}
	totalText := strings.TrimSpace(text[idx+len(marker):])
	total, err := strconv.Atoi(totalText)
	if err != nil {
		total = countLines([]byte(text[:idx]))
	}
	return text[:idx], total, false
}
