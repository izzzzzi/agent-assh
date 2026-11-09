package cli

import (
	"context"
	"encoding/json"
	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/safety"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newTransferListCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var path string
	cmd := &cobra.Command{
		Use:           "list",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if path == "" {
				path = "."
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			remoteCommand := remoteFileListCommand(path)
			result := runSSH(ctx, ssh.command(), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			entries := parseFileListJSON(result.Stdout)
			if err := transferMarkerError(cmd, result.Stdout, path, len(entries)); err != nil {
				return err
			}
			writeAudit("transfer_list", "", ssh.Host, ssh.User, remoteCommand,
				result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, map[string]any{
				"ok":      true,
				"host":    ssh.Host,
				"user":    ssh.User,
				"path":    path,
				"count":   len(entries),
				"entries": entries,
			})
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&path, "path", ".", "remote directory path")
	return cmd
}

func newTransferStatCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var path string
	cmd := &cobra.Command{
		Use:           "stat",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if path == "" {
				return writeInvalidArgs(cmd, "--path is required", "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			remoteCommand := remoteFileStatCommand(path)
			result := runSSH(ctx, ssh.command(), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			if err := transferMarkerError(cmd, result.Stdout, path, 0); err != nil {
				return err
			}
			stat := parseFileStatJSON(result.Stdout)
			writeAudit("transfer_stat", "", ssh.Host, ssh.User, remoteCommand,
				result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			resultMap := map[string]any{
				"ok":   true,
				"host": ssh.Host,
				"user": ssh.User,
				"path": path,
			}
			for k, v := range stat {
				resultMap[k] = v
			}
			return writeJSON(cmd, resultMap)
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&path, "path", "", "remote file/directory path")
	return cmd
}

func newTransferMkdirCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var path string
	cmd := &cobra.Command{
		Use:           "mkdir",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if path == "" {
				return writeInvalidArgs(cmd, "--path is required", "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			remoteCommand := "mkdir -p " + shellRemotePath(path)
			result := runSSH(ctx, ssh.command(), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.ExitCode != 0 {
				return writeError(cmd, "mkdir_failed", "mkdir exited with code "+strconv.Itoa(result.ExitCode), "")
			}
			writeAudit("transfer_mkdir", "", ssh.Host, ssh.User, remoteCommand,
				result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, map[string]any{
				"ok":   true,
				"host": ssh.Host,
				"user": ssh.User,
				"path": path,
			})
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&path, "path", "", "remote directory path")
	return cmd
}

func newTransferRmCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var path string
	var recursive bool
	cmd := &cobra.Command{
		Use:           "rm",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if path == "" {
				return writeInvalidArgs(cmd, "--path is required", "")
			}

			// transfer rm has no --confirm-danger, so a system path is refused
			// outright rather than removed without the classifier seeing it.
			if r := safety.CheckCommand("rm " + remote.SingleQuote(path)); r.Dangerous {
				return writeError(cmd, "dangerous_command_requires_confirmation",
					"transfer rm refuses system paths ("+r.Rule+")",
					"if intentional, use `assh session exec -s SID --confirm-danger -- \"rm ...\"`")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			flags := ""
			if recursive {
				flags = "-rf "
			}
			remoteCommand := "rm " + flags + shellRemotePath(path)
			result := runSSH(ctx, ssh.command(), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.ExitCode != 0 {
				return writeError(cmd, "rm_failed", "rm exited with code "+strconv.Itoa(result.ExitCode), "")
			}
			writeAudit("transfer_rm", "", ssh.Host, ssh.User, remoteCommand,
				result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, map[string]any{
				"ok":   true,
				"host": ssh.Host,
				"user": ssh.User,
				"path": path,
			})
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&path, "path", "", "remote file/directory path")
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "remove recursively")
	return cmd
}

func newTransferMvCommand() *cobra.Command {
	ssh := defaultSSHOptions()
	var source string
	var dest string
	cmd := &cobra.Command{
		Use:           "mv",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}
			if source == "" || dest == "" {
				return writeInvalidArgs(cmd, "--source and --dest are required", "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			remoteCommand := "mv " + shellRemotePath(source) + " " + shellRemotePath(dest)
			result := runSSH(ctx, ssh.command(), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.ExitCode != 0 {
				return writeError(cmd, "mv_failed", "mv exited with code "+strconv.Itoa(result.ExitCode), "")
			}
			writeAudit("transfer_mv", "", ssh.Host, ssh.User, remoteCommand,
				result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, map[string]any{
				"ok":     true,
				"host":   ssh.Host,
				"user":   ssh.User,
				"source": source,
				"dest":   dest,
			})
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	cmd.Flags().StringVar(&source, "source", "", "source path")
	cmd.Flags().StringVar(&dest, "dest", "", "destination path")
	return cmd
}

// Both commands report a missing path and a userland without GNU find/stat
// explicitly. Answering `[]` or `{}` there would read as "empty directory" and
// send the caller down the wrong path.
const (
	transferNotFoundMarker    = "__ASSH_NOT_FOUND__"
	transferUnsupportedMarker = "__ASSH_UNSUPPORTED__"
)

func remoteFileListCommand(path string) string {
	p := shellRemotePath(path)
	return `test -e ` + p + ` || { echo ` + transferNotFoundMarker + `; exit 0; }; find ` + p +
		` -mindepth 1 -maxdepth 1 -printf '{"name":"%f","type":"%Y","size":%s,` +
		`"mtime":"%TY-%Tm-%TdT%TH:%TM:%TSZ"}\n' 2>/dev/null || echo ` + transferUnsupportedMarker
}

func remoteFileStatCommand(path string) string {
	p := shellRemotePath(path)
	return `test -e ` + p + ` || { echo ` + transferNotFoundMarker + `; exit 0; }; ` +
		`stat --format='{"name":"%n","size":%s,"type":"%F",` +
		`"mode":"%a","uid":%u,"gid":%g,"mtime":"%y"}' ` + p + ` 2>/dev/null || echo ` + transferUnsupportedMarker
}

// transferMarkerError turns a marker in list/stat stdout into a typed error.
// A partial listing (some entries, then a find error) is not an error.
func transferMarkerError(cmd *cobra.Command, stdout []byte, path string, parsed int) error {
	text := string(stdout)
	switch {
	case strings.Contains(text, transferNotFoundMarker):
		return writeError(cmd, "remote_file_not_found", "no such file or directory: "+path, "")
	case parsed == 0 && strings.Contains(text, transferUnsupportedMarker):
		return writeError(cmd, "unsupported_remote",
			"remote find/stat lack the GNU options this command needs",
			"use `assh exec -H HOST -u USER -- \"ls -la PATH\"` on this host")
	}
	return nil
}

func parseFileListJSON(stdout []byte) []any {
	var entries []any
	lines := splitLines(stdout)
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	if entries == nil {
		entries = make([]any, 0)
	}
	return entries
}

func parseFileStatJSON(stdout []byte) map[string]any {
	var stat map[string]any
	text := string(stdout)
	if err := json.Unmarshal([]byte(text), &stat); err != nil {
		return map[string]any{"raw": text}
	}
	return stat
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	text := string(data)
	lines := make([]string, 0)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}
