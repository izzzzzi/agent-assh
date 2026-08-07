package safety

import "strings"

// Rule detectors for the safety classifier.
//
// These helpers examine tokenized command segments and report whether they
// match a destructive pattern (recursive deletes, critical-path overwrites,
// dangerous dd targets, redirects into system directories). They are called
// from checkSegment in safety.go; this file keeps the pattern-matching
// helpers separate from the shell-parsing machinery.

// hasRecursiveFlag reports whether the token stream contains a -r/-R/--recursive
// flag, which combined with rm/chmod/chown flags a recursive destructive op.
func hasRecursiveFlag(tokens []token) bool {
	for _, tok := range tokens {
		if !strings.HasPrefix(tok.Value, "-") {
			continue
		}
		if strings.HasPrefix(tok.Value, "--") {
			if tok.Value == "--recursive" {
				return true
			}
			continue
		}
		if strings.Contains(tok.Value, "r") || strings.Contains(tok.Value, "R") {
			return true
		}
	}
	return false
}

func hasLiteralArg(tokens []token, value string) bool {
	for _, tok := range tokens {
		if tok.Value == value {
			return true
		}
	}
	return false
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// hasCriticalPath reports whether any non-flag token references a system
// directory (/, /etc, /var, /usr, ...). Such paths combined with rm/chmod/dd
// are treated as destructive because they can brick the host.
func hasCriticalPath(tokens []token) bool {
	for _, tok := range tokens {
		if strings.HasPrefix(tok.Value, "-") {
			continue
		}
		if criticalPath(tok.Value) {
			return true
		}
	}
	return false
}

func criticalPath(path string) bool {
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}
	if path == "/" || path == "/*" {
		return true
	}
	// /dev/null, /dev/zero, /dev/random, /dev/urandom are harmless sinks.
	switch path {
	case "/dev/null", "/dev/zero", "/dev/random", "/dev/urandom",
		"/dev/stdin", "/dev/stdout", "/dev/stderr", "/dev/fd":
		return false
	}
	for _, prefix := range []string{
		"/etc", "/var", "/home", "/root", "/usr", "/bin", "/sbin",
		"/lib", "/opt", "/srv", "/dev", "/boot", "/sys", "/proc",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// hasDangerousDDOutput reports whether a dd command targets a block device or
// an absolute path (of=/dev/sda, of=/etc/passwd, ...) which can destroy data.
func hasDangerousDDOutput(tokens []token) bool {
	for _, tok := range tokens {
		if !strings.HasPrefix(tok.Value, "of=") {
			continue
		}
		output := strings.TrimPrefix(tok.Value, "of=")
		if strings.HasPrefix(output, "/dev/") || strings.HasPrefix(output, "/") {
			return true
		}
		for _, dev := range []string{
			"/dev/sd", "/dev/nvme", "/dev/hd", "/dev/xvd", "/dev/vd",
			"/dev/mmcblk", "/dev/md", "/dev/dm-", "/dev/loop", "/dev/disk",
		} {
			if strings.HasPrefix(output, dev) {
				return true
			}
		}
	}
	return false
}

// hasDangerousRedirect reports whether a redirect (>, 2>, 1>|, ...) writes into
// a critical system path. Redirects into user files are allowed.
func hasDangerousRedirect(tokens []token) bool {
	for i, tok := range tokens {
		if tok.Quoted {
			continue
		}

		target := ""
		switch {
		case isRedirectOperator(tok.Value):
			if i+1 < len(tokens) {
				target = tokens[i+1].Value
			}
		default:
			target = attachedRedirectTarget(tok.Value)
		}

		if target != "" && criticalPath(target) {
			return true
		}
	}
	return false
}

func attachedRedirectTarget(value string) string {
	for _, marker := range []string{"2>|", "1>|", ">|", "2>", "1>", ">"} {
		if index := strings.Index(value, marker); index >= 0 {
			return value[index+len(marker):]
		}
	}
	return ""
}

func isRedirectOperator(value string) bool {
	if value == ">" || value == ">|" {
		return true
	}
	if strings.HasSuffix(value, ">") && isDigits(strings.TrimSuffix(value, ">")) {
		return true
	}
	if strings.HasSuffix(value, ">|") && isDigits(strings.TrimSuffix(value, ">|")) {
		return true
	}
	return false
}
