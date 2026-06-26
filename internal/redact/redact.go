// Package redact provides best-effort secret hygiene for remote command output.
//
// This is NOT a security boundary. Regex matching misses the long tail of secret
// formats, and the raw bytes have already transited the SSH transport before they
// reach this package. Redaction lowers the chance that obvious credentials land in
// an LLM agent's context or in the local output store; treat it as hygiene, not a
// guarantee.
//
// All replacements are line-count-preserving: a redacted token never spans or
// removes a newline. This keeps the pagination contract in state.OutputStore
// (total_lines/offset/has_more, computed on stored lines) consistent before and
// after redaction.
package redact

import (
	"regexp"
	"strings"
)

type pattern struct {
	label string
	re    *regexp.Regexp
}

// patterns are precompiled at package init. Each regexp matches within a single
// line (no newline in the class) so replacements preserve line count.
var patterns = []pattern{
	// AWS access key id.
	{"aws_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	// JWT (three base64url segments).
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
	// Bearer tokens in headers/log lines.
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-+/=]{12,}`)},
	// key=value / key: value assignments for sensitive keys. Captures the key so
	// it is preserved; only the value is masked.
	{"secret_assignment", regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|client[_-]?secret)\b(\s*[:=]\s*)("[^"\n]*"|'[^'\n]*'|[^\s"';,)]+)`)},
}

var (
	pemBegin = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	pemEnd   = regexp.MustCompile(`-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

// Result reports how a buffer was redacted.
type Result struct {
	// Count is the number of individual secret matches that were masked.
	Count int
}

// Bytes redacts secrets in data, returning the masked copy and a Result. The line
// count of the returned slice equals the line count of the input.
func Bytes(data []byte) ([]byte, Result) {
	if len(data) == 0 {
		return data, Result{}
	}
	out, res := String(string(data))
	return []byte(out), res
}

// String redacts secrets in s, returning the masked string and a Result.
func String(s string) (string, Result) {
	if s == "" {
		return s, Result{}
	}
	count := 0
	s, n := redactPEMBlocks(s)
	count += n

	for _, p := range patterns {
		if p.label == "secret_assignment" {
			s = p.re.ReplaceAllStringFunc(s, func(match string) string {
				groups := p.re.FindStringSubmatch(match)
				if len(groups) != 4 {
					return match
				}
				count++
				return groups[1] + groups[2] + "[REDACTED:secret]"
			})
			continue
		}
		label := p.label
		s = p.re.ReplaceAllStringFunc(s, func(string) string {
			count++
			return "[REDACTED:" + label + "]"
		})
	}
	return s, Result{Count: count}
}

// redactPEMBlocks masks every line of a PEM private-key block (the BEGIN/END
// markers and the base64 body) while preserving line count. Counts one redaction
// per block. An unterminated block (BEGIN with no END) is masked through the end
// of the buffer.
func redactPEMBlocks(s string) (string, int) {
	if !pemBegin.MatchString(s) {
		return s, 0
	}
	lines := strings.Split(s, "\n")
	count := 0
	inBlock := false
	for i, line := range lines {
		switch {
		case !inBlock && pemBegin.MatchString(line):
			inBlock = true
			count++
			lines[i] = "[REDACTED:pem]"
		case inBlock && pemEnd.MatchString(line):
			inBlock = false
			lines[i] = "[REDACTED:pem]"
		case inBlock:
			lines[i] = "[REDACTED:pem]"
		}
	}
	return strings.Join(lines, "\n"), count
}
