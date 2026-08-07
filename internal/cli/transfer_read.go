package cli

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/ids"
	"github.com/izzzzzi/agent-assh/internal/redact"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/state"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

const (
	transferReadMarker      = "__ASSH_RF__"
	transferReadDefaultMax  = 1 << 20 // 1 MiB
	transferReadProbeWindow = 65536   // bytes inspected for binary detection
)

func newTransferReadCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var path string
	var maxBytes int64
	var noRedact bool
	cmd := &cobra.Command{
		Use:           "read",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransferRead(cmd, ssh, path, maxBytes, noRedact)
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&path, "path", "", "remote file path")
	cmd.Flags().Int64Var(&maxBytes, "max-bytes", transferReadDefaultMax, "refuse files larger than this many bytes")
	cmd.Flags().BoolVar(&noRedact, "no-redact", false, "disable best-effort secret redaction in stored output")
	return cmd
}

func runTransferRead(cmd *cobra.Command, ssh sshOptions, path string, maxBytes int64, noRedact bool) error {
	if err := ssh.validate(true); err != nil {
		return writeInvalidArgs(cmd, err.Error(), "")
	}
	if path == "" {
		return writeInvalidArgs(cmd, "--path is required", "")
	}
	if maxBytes < 1 {
		return writeInvalidArgs(cmd, "--max-bytes must be at least 1", "")
	}

	outputID, err := ids.New()
	if err != nil {
		return writeError(cmd, "internal_error", err.Error(), "")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
	defer cancel()

	remoteCommand := remoteFileReadCommand(path, maxBytes)
	result := runSSH(ctx, ssh.command(), remoteCommand)
	if code := sshResultErrorCode(ctx.Err(), result); code != "" {
		return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
	}

	status, content := parseTransferRead(result.Stdout)
	if err := transferReadStatusError(cmd, status, path, ssh, maxBytes); err != nil {
		return err
	}
	if status != "ok" {
		return writeError(cmd, "transfer_failed", sshResultErrorMessage(ctx.Err(), result), "")
	}

	return finishTransferRead(cmd, ssh, path, outputID, content, noRedact, remoteCommand, result)
}

// transferReadStatusError maps a remote read status to a typed assh error with
// a hint pointing at a useful follow-up command. Returns nil when status == ok.
func transferReadStatusError(cmd *cobra.Command, status, path string, ssh sshOptions, maxBytes int64) error {
	stat := "assh transfer stat -H " + ssh.Host + " -u " + ssh.User + " --path " + path
	switch {
	case status == "notfound":
		return writeError(cmd, "remote_file_not_found", path+": no such file on "+ssh.Host, "verify with: "+stat)
	case status == "dir":
		return writeError(cmd, "not_a_file", path+" is a directory",
			"list it with: assh transfer list -H "+ssh.Host+" -u "+ssh.User+" --path "+path)
	case status == "noperm":
		return writeError(cmd, "permission_denied", path+": permission denied", "check ownership with: "+stat)
	case status == "binary":
		return writeError(cmd, "binary_file", path+" appears to be binary",
			"download it instead with: assh transfer get -H "+ssh.Host+" -u "+ssh.User+" "+path+" ./local")
	case strings.HasPrefix(status, "toolarge:"):
		size := strings.TrimPrefix(status, "toolarge:")
		return writeError(cmd, "file_too_large",
			path+" is "+size+" bytes (limit "+strconv.FormatInt(maxBytes, 10)+")",
			"raise --max-bytes, or fetch the whole file with: assh transfer get -H "+
				ssh.Host+" -u "+ssh.User+" "+path+" ./local")
	}
	return nil
}

func finishTransferRead(
	cmd *cobra.Command, ssh sshOptions, path, outputID, content string,
	noRedact bool, remoteCommand string, result transport.Result,
) error {
	redactionCount := 0
	if !noRedact {
		var redRes redact.Result
		content, redRes = redact.String(content)
		redactionCount = redRes.Count
	}

	store := state.NewOutputStore(filepath.Join(stateBaseDir(), "outputs"))
	if err := store.Write(outputID, []byte(content), nil); err != nil {
		return writeError(cmd, "internal_error", err.Error(), "")
	}
	lines := countLines([]byte(content))
	writeAudit("transfer_read", "", ssh.Host, ssh.User, remoteCommand, result.ExitCode, lines, 0)

	return writeJSON(cmd, map[string]any{
		"ok":              true,
		"host":            ssh.Host,
		"user":            ssh.User,
		"path":            path,
		"output_id":       outputID,
		"stdout_lines":    lines,
		"redacted":        redactionCount > 0,
		"redaction_count": redactionCount,
		"next_commands": map[string]string{
			"read": "assh read --id " + outputID + " --limit 50",
		},
	})
}

// remoteFileReadCommand emits a single status line (prefixed with the marker)
// describing the file, followed by the raw file content when the status is ok.
// It performs existence, type, permission, size, and binary checks in one round
// trip without depending on SFTP.
func remoteFileReadCommand(path string, maxBytes int64) string {
	p := remote.SingleQuote(path)
	limit := strconv.FormatInt(maxBytes, 10)
	window := strconv.Itoa(transferReadProbeWindow)
	return strings.Join([]string{
		`P=` + p + `;`,
		`if [ ! -e "$P" ]; then echo '` + transferReadMarker + `notfound'; exit 0; fi;`,
		`if [ -d "$P" ]; then echo '` + transferReadMarker + `dir'; exit 0; fi;`,
		`if [ ! -r "$P" ]; then echo '` + transferReadMarker + `noperm'; exit 0; fi;`,
		`SZ=$(wc -c < "$P" 2>/dev/null | tr -d ' ');`,
		`if [ "$SZ" -gt ` + limit + ` ] 2>/dev/null; then echo '` + transferReadMarker + `toolarge:'"$SZ"; exit 0; fi;`,
		`RB=$(head -c ` + window + ` "$P" | wc -c | tr -d ' ');`,
		`NB=$(head -c ` + window + ` "$P" | LC_ALL=C tr -d '\000' | wc -c | tr -d ' ');`,
		`if [ "$RB" != "$NB" ]; then echo '` + transferReadMarker + `binary'; exit 0; fi;`,
		`echo '` + transferReadMarker + `ok'; cat "$P"`,
	}, " ")
}

// parseTransferRead splits the marker status line from the file content.
func parseTransferRead(stdout []byte) (status, content string) {
	text := string(stdout)
	idx := strings.Index(text, "\n")
	var first string
	if idx < 0 {
		first = text
		text = ""
	} else {
		first = text[:idx]
		text = text[idx+1:]
	}
	first = strings.TrimRight(first, "\r")
	if !strings.HasPrefix(first, transferReadMarker) {
		return "", string(stdout)
	}
	return strings.TrimPrefix(first, transferReadMarker), text
}
