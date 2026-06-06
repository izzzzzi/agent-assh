package cli

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
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
			writeAudit("transfer_list", "", ssh.Host, ssh.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

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

			stat := parseFileStatJSON(result.Stdout)
			writeAudit("transfer_stat", "", ssh.Host, ssh.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

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

			remoteCommand := "mkdir -p " + remote.SingleQuote(path)
			result := runSSH(ctx, ssh.command(), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.ExitCode != 0 {
				return writeError(cmd, "mkdir_failed", "mkdir exited with code "+strconv.Itoa(result.ExitCode), "")
			}
			writeAudit("transfer_mkdir", "", ssh.Host, ssh.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

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

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			flags := ""
			if recursive {
				flags = "-rf "
			}
			remoteCommand := "rm " + flags + remote.SingleQuote(path)
			result := runSSH(ctx, ssh.command(), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.ExitCode != 0 {
				return writeError(cmd, "rm_failed", "rm exited with code "+strconv.Itoa(result.ExitCode), "")
			}
			writeAudit("transfer_rm", "", ssh.Host, ssh.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

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

			remoteCommand := "mv " + remote.SingleQuote(source) + " " + remote.SingleQuote(dest)
			result := runSSH(ctx, ssh.command(), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.ExitCode != 0 {
				return writeError(cmd, "mv_failed", "mv exited with code "+strconv.Itoa(result.ExitCode), "")
			}
			writeAudit("transfer_mv", "", ssh.Host, ssh.User, remoteCommand, result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

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

func remoteFileListCommand(path string) string {
	return `find ` + remote.SingleQuote(path) + ` -maxdepth 1 -printf '{"name":"%f","type":"%Y","size":%s,"mtime":"%TY-%Tm-%TdT%TH:%TM:%TSZ"}\n' 2>/dev/null || echo "[]"`
}

func remoteFileStatCommand(path string) string {
	return `stat --format='{"name":"%n","size":%s,"type":"%F","mode":"%a","uid":%u,"gid":%g,"mtime":"%y"}' ` + remote.SingleQuote(path) + ` 2>/dev/null || echo "{}"`
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
