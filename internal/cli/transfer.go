package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/izzzzzi/agent-assh/internal/transport"
	"github.com/spf13/cobra"
)

func newTransferCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "transfer",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "transfer subcommand required", "run assh transfer --help")
		},
	}
	cmd.AddCommand(
		newTransferPutCommand(),
		newTransferGetCommand(),
		newTransferReadCommand(),
		newTransferListCommand(),
		newTransferStatCommand(),
		newTransferMkdirCommand(),
		newTransferRmCommand(),
		newTransferMvCommand(),
		newTransferSyncCommand(),
	)
	return cmd
}

func newTransferPutCommand() *cobra.Command {
	return newTransferLeafCommand("put", transport.Upload)
}

func newTransferGetCommand() *cobra.Command {
	return newTransferLeafCommand("get", transport.Download)
}

func newTransferLeafCommand(use string, direction transport.SCPDirection) *cobra.Command {
	ssh := defaultSSHOptions()
	cmd := &cobra.Command{
		Use:           use + " SOURCE DEST",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return writeInvalidArgs(cmd, "source and destination required", "")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			destination := args[1]

			resolveTransferEndpoints(&ssh, &source, &destination, direction)
			if ssh.Host == "" {
				return writeInvalidArgs(cmd, "host required", transferHostHint(use, direction))
			}
			if err := ssh.validate(true); err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}

			bytes, err := transferSizeBefore(direction, source)
			if err != nil {
				return writeInvalidArgs(cmd, err.Error(), "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()

			// Create the destination parent directory before scp: scp refuses
			// to upload into a missing directory ("dest open: No such file or
			// directory"). A trailing-slash destination is treated as a
			// directory, so it is created itself and the file lands inside it.
			if direction == transport.Upload {
				if parent, ok := remoteUploadParent(destination); ok {
					mkdirResult := runSSH(ctx, ssh.command(), remoteMkdirCommand(parent))
					if code := sshResultErrorCode(ctx.Err(), mkdirResult); code != "" {
						return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), mkdirResult), "")
					}
					if mkdirResult.ExitCode != 0 {
						return writeError(cmd, "mkdir_failed",
							strings.TrimSpace(string(mkdirResult.Stderr)),
							"could not create remote destination directory")
					}
				}
			} else if parent := filepath.Dir(destination); parent != "." && parent != string(filepath.Separator) {
				if err := os.MkdirAll(parent, 0o755); err != nil {
					return writeError(cmd, "transfer_failed", err.Error(), "")
				}
			}

			result := runSCP(ctx, scpCommandFromSSHOptions(ssh), source, destination, direction)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}
			if result.Err != nil || result.ExitCode != 0 {
				return writeError(cmd, "transfer_failed", sshResultErrorMessage(ctx.Err(), result), "")
			}

			if direction == transport.Download {
				bytes, err = localFileSize(destination, "local destination")
				if err != nil {
					return writeError(cmd, "transfer_failed", err.Error(), "")
				}
			}
			writeAudit("transfer_"+use, "", ssh.Host, ssh.User, "transfer "+use,
				result.ExitCode, countLines(result.Stdout), countLines(result.Stderr))

			return writeJSON(cmd, response.OK{
				"ok":          true,
				"host":        ssh.Host,
				"user":        ssh.User,
				"source":      source,
				"destination": destination,
				"bytes":       bytes,
			})
		},
	}
	bindSSHOptions(cmd, &ssh, standardSSHOptionFlags())
	return cmd
}

// resolveTransferEndpoints accepts the scp form `[user@]host:path` on the remote
// side when no explicit -H was given, so a command copied from an ssh/scp recipe
// works without being rewritten into flags. The remote argument is reduced to
// the bare path; the host and optional user move onto the SSH options.
func resolveTransferEndpoints(ssh *sshOptions, source, destination *string, direction transport.SCPDirection) {
	if ssh.Host != "" {
		return
	}
	remoteArg := destination
	if direction == transport.Download {
		remoteArg = source
	}
	user, host, path, ok := parseRemoteSpec(*remoteArg)
	if !ok {
		return
	}
	ssh.Host = host
	if user != "" {
		ssh.User = user
	}
	*remoteArg = path
}

// transferHostHint names the working form for the direction that failed, so the
// error can be copied verbatim instead of leaving the caller to guess.
func transferHostHint(use string, direction transport.SCPDirection) string {
	if direction == transport.Download {
		return "pass -H HOST, or use the scp form: assh transfer " + use +
			" user@host:/remote/path /local/path"
	}
	return "pass -H HOST, or use the scp form: assh transfer " + use +
		" /local/path user@host:/remote/path"
}

// parseRemoteSpec splits an scp-style [user@]host:path argument. It reports
// ok=false for anything that is plainly a local path, so the caller can fall
// back to requiring -H.
//
// A colon only introduces the host when it appears before any slash: that keeps
// local paths containing colons (and URLs) from being read as remote specs. A
// single-letter "host" followed by a rooted path is a Windows drive letter, not
// a host, so it is rejected too.
func parseRemoteSpec(spec string) (user, host, path string, ok bool) {
	colon := strings.Index(spec, ":")
	if colon <= 0 {
		return "", "", "", false
	}
	if strings.Contains(spec[:colon], "/") {
		return "", "", "", false
	}

	hostPart := spec[:colon]
	path = spec[colon+1:]
	if at := strings.LastIndex(hostPart, "@"); at >= 0 {
		user = hostPart[:at]
		hostPart = hostPart[at+1:]
	}
	if hostPart == "" || path == "" {
		return "", "", "", false
	}
	if len(hostPart) == 1 && (strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`)) {
		return "", "", "", false
	}
	return user, hostPart, path, true
}

// remoteUploadParent returns the remote directory that must exist before an
// scp upload to destination, and whether creating it is needed.
func remoteUploadParent(destination string) (string, bool) {
	dir := strings.TrimRight(destination, "/")
	if dir == "" {
		return "", false
	}
	if strings.HasSuffix(destination, "/") {
		// Destination is a directory itself: create it so the file lands inside.
		return dir, true
	}
	i := strings.LastIndexByte(dir, '/')
	if i < 0 {
		return "", false // bare filename, no parent
	}
	parent := dir[:i]
	if parent == "" || parent == "/" {
		return "", false // root always exists
	}
	return parent, true
}

// remoteMkdirCommand builds a `mkdir -p` command for a remote path. A leading
// `~/` is expanded outside single quotes so the remote shell resolves $HOME.
func remoteMkdirCommand(path string) string {
	if strings.HasPrefix(path, "~/") {
		return "mkdir -p ~/" + remote.SingleQuote(path[2:])
	}
	return "mkdir -p " + remote.SingleQuote(path)
}

func transferSizeBefore(direction transport.SCPDirection, source string) (int64, error) {
	if direction != transport.Upload {
		return 0, nil
	}
	return localFileSize(source, "local source")
}

func localFileSize(path string, label string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, validationError(label + " is not readable")
	}
	if info.IsDir() {
		return 0, validationError(label + " must be a file")
	}
	return info.Size(), nil
}

func scpCommandFromSSHOptions(ssh sshOptions) transport.SCPCommand {
	return transport.SCPCommand{
		Host:          ssh.Host,
		User:          ssh.User,
		Port:          ssh.Port,
		Identity:      ssh.Identity,
		Jump:          ssh.Jump,
		TimeoutSecond: ssh.TimeoutSecond,
		HostKeyPolicy: ssh.HostKeyPolicy,
		Batch:         ssh.Batch,
	}
}

var runSCP = func(ctx context.Context, command transport.SCPCommand,
	source, destination string, direction transport.SCPDirection) transport.Result {
	return command.Run(ctx, source, destination, direction)
}
