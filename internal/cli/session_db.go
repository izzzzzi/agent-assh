package cli

import (
	"context"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
	"github.com/izzzzzi/agent-assh/internal/session"
	"github.com/spf13/cobra"
)

func newSessionDBQueryCommand() *cobra.Command {
	var sid string
	var dbType string
	var database string
	var query string
	var dbUser string
	var dbPass string
	var dbHost string
	var dbPort int
	ssh := defaultSSHOptions()

	cmd := &cobra.Command{
		Use:           "db-query",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noPositionalArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !remote.SafeSID(sid) {
				return writeInvalidArgs(cmd, "--sid is required", "")
			}
			if query == "" {
				return writeInvalidArgs(cmd, "--query is required", "")
			}

			if !isReadOnlyQuery(strings.ToUpper(strings.TrimSpace(query))) {
				return writeError(cmd, "dangerous_command_requires_confirmation",
					"query looks like a write operation; only SELECT/SHOW/DESCRIBE/EXPLAIN allowed",
					"db-query is read-only by design")
			}

			entry, err := session.LoadRegistry(stateBaseDir(), sid)
			if err != nil {
				return writeError(cmd, "session_not_found", err.Error(), "")
			}

			remoteCommand, errMsg := dbRemoteCommand(dbType, database, query, dbUser, dbPass, dbHost, dbPort)
			if errMsg != "" {
				return writeInvalidArgs(cmd, errMsg, "")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(ssh.TimeoutSecond)*time.Second)
			defer cancel()
			result := runSSH(ctx, sessionSSH(entry.Host, entry.User, entry.Port, entry.Identity, ssh.Jump, ssh.TimeoutSecond, entry.HostKeyPolicy), remoteCommand)
			if code := sshResultErrorCode(ctx.Err(), result); code != "" {
				return writeError(cmd, code, sshResultErrorMessage(ctx.Err(), result), "")
			}

			return writeJSON(cmd, map[string]any{
				"ok":       true,
				"sid":      sid,
				"db_type":  dbType,
				"database": database,
				"output":   string(result.Stdout),
			})
		},
	}

	cmd.Flags().StringVarP(&sid, "sid", "s", "", "session id")
	cmd.Flags().StringVar(&dbType, "type", "mysql", "database type: mysql, postgres")
	cmd.Flags().StringVarP(&database, "database", "d", "", "database name")
	cmd.Flags().StringVarP(&query, "query", "q", "", "SQL query (SELECT only)")
	cmd.Flags().StringVarP(&dbUser, "db-user", "U", "", "database user")
	cmd.Flags().StringVarP(&dbPass, "db-pass", "W", "", "database password (prefer env vars on remote)")
	cmd.Flags().StringVar(&dbHost, "db-host", "localhost", "database host")
	cmd.Flags().IntVar(&dbPort, "db-port", 0, "database port")
	bindSSHOptions(cmd, &ssh, sshOptionFlags{jump: true})
	return cmd
}

func dbRemoteCommand(dbType, database, query, dbUser, dbPass, dbHost string, dbPort int) (string, string) {
	switch dbType {
	case "mysql", "mariadb":
		return mysqlQueryCommand(database, query, dbUser, dbPass, dbHost, dbPort), ""
	case "postgres", "postgresql", "pg":
		return pgQueryCommand(database, query, dbUser, dbPass, dbHost, dbPort), ""
	default:
		return "", "unsupported database type: " + dbType
	}
}

func mysqlQueryCommand(database, query, dbUser, dbPass, dbHost string, dbPort int) string {
	parts := []string{"mysql", "-N", "-B"}
	if dbHost != "" && dbHost != "localhost" {
		parts = append(parts, "-h", remote.SingleQuote(dbHost))
	}
	if dbPort > 0 {
		parts = append(parts, "-P", itoaStr(dbPort))
	}
	if dbUser != "" {
		parts = append(parts, "-u", remote.SingleQuote(dbUser))
	}
	if dbPass != "" {
		parts = append(parts, "-p"+remote.SingleQuote(dbPass))
	}
	if database != "" {
		parts = append(parts, remote.SingleQuote(database))
	}
	parts = append(parts, "-e", remote.SingleQuote(query))
	return strings.Join(parts, " ") + " 2>&1"
}

func pgQueryCommand(database, query, dbUser, dbPass, dbHost string, dbPort int) string {
	env := ""
	if dbPass != "" {
		env = "PGPASSWORD=" + remote.SingleQuote(dbPass) + " "
	}
	parts := []string{env + "psql", "-A", "-t", "-q"}
	if dbHost != "" && dbHost != "localhost" {
		parts = append(parts, "-h", remote.SingleQuote(dbHost))
	}
	if dbPort > 0 {
		parts = append(parts, "-p", itoaStr(dbPort))
	}
	if dbUser != "" {
		parts = append(parts, "-U", remote.SingleQuote(dbUser))
	}
	if database != "" {
		parts = append(parts, "-d", remote.SingleQuote(database))
	}
	parts = append(parts, "-c", remote.SingleQuote(query))
	return strings.Join(parts, " ") + " 2>&1"
}

func isReadOnlyQuery(query string) bool {
	query = strings.TrimSpace(query)
	for _, prefix := range []string{"SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN"} {
		if strings.HasPrefix(query, prefix+" ") || query == prefix {
			return true
		}
	}
	return false
}

func itoaStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
