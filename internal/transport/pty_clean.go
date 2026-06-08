package transport

import (
	"regexp"
	"strings"
)

var (
	// ANSI escape sequences — cursor movement, colors, erase display/line, DEC private modes,
	// alternate screen, device attributes
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;?><]*[a-zA-Z]|\x1b\][^\x1b\x07]*(\x07|\x1b\\)`)

	// Carriage returns — replace with newlines for consistent processing
	crRe = regexp.MustCompile(`\r\n?`)
)

// CleanPTYOutput strips PTY noise from command output.
// Handles: ANSI escapes, carriage returns, shell prompts, echoing of the
// sent command, gateway banners, and trailing whitespace.
func CleanPTYOutput(raw []byte, sentCommand string) []byte {
	s := string(raw)
	if s == "" {
		return raw
	}

	// Step 1: normalize line endings (CRLF → LF, bare CR → LF)
	s = crRe.ReplaceAllString(s, "\n")

	// Step 2: strip ANSI escape sequences
	s = ansiRe.ReplaceAllString(s, "")

	// Step 3: strip trailing shell prompts on their own lines
	s = regexp.MustCompile(`(?m)^[a-z]+@[a-zA-Z0-9_-]+:[^\$#]*[\$#]\s*\n?`).ReplaceAllString(s, "")

	// Step 4: strip lines that exactly match the sent command (PTY echo)
	if sentCommand != "" {
		cmdLine := strings.TrimSpace(sentCommand)
		lines := strings.Split(s, "\n")
		clean := make([]string, 0, len(lines))
		for _, line := range lines {
			trimmed := strings.TrimRight(line, "\r ")
			// Strip command echo
			if trimmed == cmdLine || trimmed == "$ "+cmdLine {
				continue
			}
			// Strip partial command echoes (terminal width truncation)
			if len(trimmed) > len(cmdLine)/2 && strings.Contains(trimmed, cmdLine[:len(cmdLine)/2]) {
				continue
			}
			// Strip "exit" echo from the forced exit command
			if trimmed == "exit" || trimmed == "$ exit" {
				continue
			}
			// Strip empty shell prompts (just "$" or "#" on a line)
			if trimmed == "$" || trimmed == "#" {
				continue
			}
			clean = append(clean, line)
		}
		s = strings.Join(clean, "\n")
	}

	// Step 5: strip RunPod-style gateway banners
	s = regexp.MustCompile(`(?m)^-- RUNPOD\.IO --$`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?m)^Enjoy your Pod #[a-zA-Z0-9]+ \^_\^$`).ReplaceAllString(s, "")

	// Step 6: strip "Connection to ... closed" messages
	s = regexp.MustCompile(`(?m)^Connection to .+ closed\.?$`).ReplaceAllString(s, "")

	// Step 7: strip empty leading/trailing lines
	s = strings.Trim(s, "\n\t ")

	// Step 8: collapse multiple consecutive empty lines into one
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")

	if s == "" {
		return nil
	}
	return []byte(s)
}
