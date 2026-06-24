package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/response"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCommand() *cobra.Command {
	var checkUpdate bool

	cmd := &cobra.Command{
		Use:           "version",
		Short:         "Print version information as JSON",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unexpected positional arguments", "")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if checkUpdate {
				return runVersionCheck(cmd)
			}
			return writeJSON(cmd, response.OK{
				"ok":         true,
				"version":    version,
				"commit":     commit,
				"date":       date,
				"go_version": runtime.Version(),
			})
		},
	}

	cmd.Flags().BoolVar(&checkUpdate, "check", false, "check for newer version on GitHub")
	return cmd
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func runVersionCheck(cmd *cobra.Command) error {
	if version == "dev" || version == "0.0.0" {
		return writeError(cmd, "version_check_failed", "development build — no update check available", "")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/izzzzzi/agent-assh/releases/latest")
	if err != nil {
		return writeError(cmd, "version_check_failed", "cannot reach GitHub: "+err.Error(), "")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return writeError(cmd, "version_check_failed", fmt.Sprintf("GitHub API returned HTTP %d", resp.StatusCode), "")
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return writeError(cmd, "version_check_failed", "cannot parse release info: "+err.Error(), "")
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(version, "v")

	out := cmd.OutOrStdout()
	if latest != current {
		_, _ = fmt.Fprintf(out, "Update available: v%s → v%s\n", current, latest)
		_, _ = fmt.Fprintf(out, "Run: npm i -g agent-assh@latest\n")
		_, _ = fmt.Fprintf(out, "Release: %s\n", release.HTMLURL)
	} else {
		_, _ = fmt.Fprintf(out, "v%s is up to date.\n", current)
	}

	return nil
}
