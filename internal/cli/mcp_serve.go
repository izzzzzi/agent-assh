package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/izzzzzi/agent-assh/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "mcp serve",
		SilenceUsage:  true,
		SilenceErrors: true,
		Short:         "Start MCP stdio server for Claude Code, Cursor, Windsurf, etc.",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := mcp.NewServer()
			registerMCPHandlers(server)
			return server.Run()
		},
	}
	return cmd
}

func registerMCPHandlers(s *mcp.Server) {
	s.RegisterHandler("ssh_connect", handleMCPConnect)
	s.RegisterHandler("ssh_exec", handleMCPExec)
	s.RegisterHandler("ssh_read", handleMCPRead)
	s.RegisterHandler("ssh_close", handleMCPClose)
	s.RegisterHandler("ssh_scan", handleMCPScan)
}

func handleMCPConnect(args map[string]any) (any, error) {
	host, _ := args["host"].(string)
	user, _ := args["user"].(string)
	port, _ := args["port"].(float64)
	identity, _ := args["identity"].(string)
	passwordEnv, _ := args["password_env"].(string)
	sessionName, _ := args["session_name"].(string)

	if sessionName == "" {
		sessionName = "default"
	}

	connectArgs := []string{"connect", "-H", host, "-u", user}
	if port > 0 {
		connectArgs = append(connectArgs, "-p", fmt.Sprintf("%d", int(port)))
	}
	if identity != "" {
		connectArgs = append(connectArgs, "-i", identity)
	}
	if passwordEnv != "" {
		connectArgs = append(connectArgs, "-E", passwordEnv, "-n", sessionName)
	} else {
		connectArgs = append(connectArgs, "-n", sessionName)
	}

	output, err := runAsshCommand(connectArgs...)
	if err != nil {
		return nil, fmt.Errorf("connect failed: %s — %v", output, err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return map[string]any{"raw_output": output}, nil
	}
	return result, nil
}

func handleMCPExec(args map[string]any) (any, error) {
	sid, _ := args["sid"].(string)
	command, _ := args["command"].(string)
	timeout, _ := args["timeout"].(float64)
	if timeout == 0 {
		timeout = 300
	}

	execArgs := []string{"session", "exec", "-s", sid, "-t", fmt.Sprintf("%d", int(timeout)), "--", command}
	output, err := runAsshCommand(execArgs...)
	if err != nil {
		return nil, fmt.Errorf("exec failed: %s — %v", output, err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return map[string]any{"raw_output": output}, nil
	}
	return result, nil
}

func handleMCPRead(args map[string]any) (any, error) {
	sid, _ := args["sid"].(string)
	seq, _ := args["seq"].(float64)
	stream, _ := args["stream"].(string)
	limit, _ := args["limit"].(float64)
	offset, _ := args["offset"].(float64)

	if stream == "" {
		stream = "stdout"
	}
	if limit == 0 {
		limit = 50
	}

	readArgs := []string{"session", "read", "-s", sid, "--seq", fmt.Sprintf("%d", int(seq)), "--stream", stream, "--limit", fmt.Sprintf("%d", int(limit)), "--offset", fmt.Sprintf("%d", int(offset))}
	output, err := runAsshCommand(readArgs...)
	if err != nil {
		return nil, fmt.Errorf("read failed: %s — %v", output, err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return map[string]any{"raw_output": output}, nil
	}
	return result, nil
}

func handleMCPClose(args map[string]any) (any, error) {
	sid, _ := args["sid"].(string)

	closeArgs := []string{"session", "close", "-s", sid}
	output, err := runAsshCommand(closeArgs...)
	if err != nil {
		return nil, fmt.Errorf("close failed: %s — %v", output, err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return map[string]any{"raw_output": output}, nil
	}
	return result, nil
}

func handleMCPScan(args map[string]any) (any, error) {
	host, _ := args["host"].(string)
	user, _ := args["user"].(string)
	identity, _ := args["identity"].(string)

	scanArgs := []string{"scan", "-H", host}
	if user != "" {
		scanArgs = append(scanArgs, "-u", user)
	}
	if identity != "" {
		scanArgs = append(scanArgs, "-i", identity)
	}

	output, err := runAsshCommand(scanArgs...)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %s — %v", output, err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return map[string]any{"raw_output": string(output)}, nil
	}
	return result, nil
}

func runAsshCommand(args ...string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = "assh"
	}

	// If running from source, try to find the binary in common locations
	if strings.Contains(exe, "go-build") || strings.Contains(exe, "Temp") {
		candidates := []string{
			filepath.Join(".", "assh"),
			filepath.Join(".", "bin", "assh"),
			filepath.Join("dist", "assh"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				exe = c
				break
			}
		}
	}

	cmd := exec.Command(exe, args...)
	// Do not inherit the MCP stdio stream
	cmd.Stdin = nil
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
