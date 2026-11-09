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
	re *regexp.Regexp
	// repl is the replacement template; capture groups keep the non-secret
	// context (key name, separator, URL user) around the masked value.
	repl string
}

// patterns are precompiled at package init. Each regexp matches within a single
// line (no newline in the class) so replacements preserve line count.
var patterns = []pattern{
	// AWS access key id.
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), "[REDACTED:aws_key]"},
	// JWT (three base64url segments).
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`), "[REDACTED:jwt]"},
	// Bearer tokens in headers/log lines.
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-+/=]{12,}`), "[REDACTED:bearer]"},
	// Password embedded in a URL: scheme://user:password@host.
	{regexp.MustCompile(`(://[^\s:/@]+:)[^\s@/]+(@)`), "${1}[REDACTED:url_password]${2}"},
	// key=value / key: value assignments for sensitive keys. The key may carry
	// an identifier prefix (DB_PASSWORD, github.token) and a closing quote
	// ("password": ...); it is preserved and only the value is masked.
	{regexp.MustCompile(`(?i)\b((?:[A-Za-z0-9]+[_.-])*(?:` +
		`secret[_-]?(?:access[_-]?)?key(?:[_-]?base)?|` +
		`password|passwd|pwd|secret|token|api[_-]?key|` +
		`access[_-]?key|private[_-]?key|client[_-]?secret)["']?)` +
		`(\s*[:=]\s*)(?:"[^"\n]*"|'[^'\n]*'|[^\s"';,)]+)`), "${1}${2}[REDACTED:secret]"},
	// Provider tokens recognisable by prefix, wherever they appear.
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}|` +
		`xox[abprs]-[A-Za-z0-9-]{10,}|sk-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{35})`), "[REDACTED:api_token]"},
}

var (
	pemBegin = regexp.MustCompile(`-{5}BEGIN [A-Z0-9 ]*PRIVATE KEY-{5}`)
	pemEnd   = regexp.MustCompile(`-{5}END [A-Z0-9 ]*PRIVATE KEY-{5}`)
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
		count += len(p.re.FindAllStringIndex(s, -1))
		s = p.re.ReplaceAllString(s, p.repl)
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
