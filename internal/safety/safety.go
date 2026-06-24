package safety

import "strings"

type Result struct {
	Dangerous bool
	Rule      string
	Message   string
}

type token struct {
	Value  string
	Quoted bool
}

const maxShellDepth = 4

// CheckCommand classifies a command using only the built-in rules.
func CheckCommand(command string) Result {
	return checkCommand(command, nil, 0)
}

// CheckCommandWithPolicy classifies a command using the built-in rules plus an
// additive deny-only policy overlay. A nil policy is equivalent to CheckCommand.
func CheckCommandWithPolicy(command string, policy *Policy) Result {
	return checkCommand(command, policy, 0)
}

func checkCommand(command string, policy *Policy, depth int) Result {
	for _, segment := range splitSegments(command) {
		if depth < maxShellDepth {
			for _, script := range commandSubstitutionScripts(segment) {
				if result := checkCommand(script, policy, depth+1); result.Dangerous {
					return result
				}
			}
		}
		tokens := shellFields(segment)
		if result := checkSegment(tokens, policy, depth); result.Dangerous {
			return result
		}
	}
	return Result{}
}

func checkSegment(tokens []token, policy *Policy, depth int) Result {
	tokens = groupCommandTokens(tokens)
	tokens = commandTokens(tokens)
	if len(tokens) == 0 {
		return Result{}
	}
	name := commandName(tokens[0].Value)
	args := tokens[1:]

	if result := policy.check(name); result.Dangerous {
		return result
	}

	switch {
	case name == "env":
		if depth < maxShellDepth {
			if script, ok := envSplitString(args); ok {
				return checkCommand(script, policy, depth+1)
			}
		}
		tokens = envCommandTokens(args)
		if len(tokens) == 0 {
			return Result{}
		}
		return checkSegment(tokens, policy, depth)
	case name == "bash" || name == "sh":
		if depth < maxShellDepth {
			if script, ok := shellCommandScript(args); ok {
				return checkCommand(script, policy, depth+1)
			}
		}
	case name == "rm":
		if hasRecursiveFlag(args) {
			return danger("rm_recursive")
		}
		if hasCriticalPath(args) {
			return danger("rm_critical_path")
		}
	case name == "find":
		if hasLiteralArg(args, "-delete") {
			return danger("find_delete")
		}
	case name == "mkfs" || strings.HasPrefix(name, "mkfs.") || name == "wipefs" || name == "shred":
		return danger("filesystem_wipe")
	case name == "dd":
		if hasDangerousDDOutput(args) {
			return danger("dd_dangerous_output")
		}
	case name == "chmod" || name == "chown" || name == "chgrp":
		if hasRecursiveFlag(args) && hasCriticalPath(args) {
			return danger("recursive_permission")
		}
	}

	if hasDangerousRedirect(tokens) {
		return danger("dangerous_redirect")
	}
	return Result{}
}

func danger(rule string) Result {
	return Result{
		Dangerous: true,
		Rule:      rule,
		Message:   "matched destructive pattern: " + rule,
	}
}

func commandName(value string) string {
	value = strings.TrimLeft(value, "{(")
	value = strings.TrimRight(value, "})")
	value = strings.TrimRight(value, "/")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		return value[index+1:]
	}
	return value
}

func groupCommandTokens(tokens []token) []token {
	if len(tokens) == 0 || tokens[0].Quoted {
		return tokens
	}
	switch tokens[0].Value {
	case "{", "(":
		tokens = tokens[1:]
	default:
		value := tokens[0].Value
		if strings.HasPrefix(value, "(") && value != "(" {
			tokens = append([]token{{Value: strings.TrimPrefix(value, "("), Quoted: tokens[0].Quoted}}, tokens[1:]...)
		}
	}
	if len(tokens) == 0 {
		return tokens
	}
	last := len(tokens) - 1
	if !tokens[last].Quoted && (tokens[last].Value == "}" || tokens[last].Value == ")") {
		return tokens[:last]
	}
	if !tokens[last].Quoted && strings.HasSuffix(tokens[last].Value, ")") && tokens[last].Value != ")" {
		tokens[last].Value = strings.TrimSuffix(tokens[last].Value, ")")
	}
	return tokens
}

func commandTokens(tokens []token) []token {
	for len(tokens) > 0 {
		value := tokens[0].Value
		switch value {
		case "sudo":
			tokens = tokens[1:]
			for len(tokens) > 0 {
				value = tokens[0].Value
				if value == "--" {
					tokens = tokens[1:]
					break
				}
				if !strings.HasPrefix(value, "-") {
					break
				}
				if sudoOptionTakesOperand(value) {
					tokens = tokens[1:]
					if len(tokens) > 0 {
						tokens = tokens[1:]
					}
					continue
				}
				tokens = tokens[1:]
			}
		case "command", "builtin", "exec", "nohup", "nice", "time", "ionice":
			tokens = tokens[1:]
		case "env":
			if _, ok := envSplitString(tokens[1:]); ok {
				return tokens
			}
			return envCommandTokens(tokens[1:])
		default:
			if isEnvAssignment(tokens[0]) {
				tokens = tokens[1:]
				continue
			}
			return tokens
		}
	}
	return tokens
}

func envCommandTokens(tokens []token) []token {
	for len(tokens) > 0 {
		value := tokens[0].Value
		if isEnvAssignment(tokens[0]) {
			tokens = tokens[1:]
			continue
		}
		if value == "--" {
			return tokens[1:]
		}
		if !strings.HasPrefix(value, "-") {
			return tokens
		}
		if envOptionTakesOperand(value) {
			tokens = tokens[1:]
			if len(tokens) > 0 {
				tokens = tokens[1:]
			}
			continue
		}
		tokens = tokens[1:]
	}
	return tokens
}

func sudoOptionTakesOperand(value string) bool {
	switch value {
	case "-u", "--user", "-g", "--group", "-h", "--host", "-p", "--prompt",
		"-D", "--chdir", "-T", "--command-timeout", "-C", "--close-from", "-R", "--chroot":
		return true
	default:
		return false
	}
}

func envOptionTakesOperand(value string) bool {
	switch value {
	case "-u", "--unset", "-C", "--chdir":
		return true
	default:
		return false
	}
}

func isEnvAssignment(tok token) bool {
	index := strings.IndexByte(tok.Value, '=')
	if index <= 0 {
		return false
	}
	for i, r := range tok.Value[:index] {
		if i == 0 {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '_' {
				return false
			}
			continue
		}
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func shellCommandScript(tokens []token) (string, bool) {
	for i := 0; i < len(tokens); i++ {
		value := tokens[i].Value
		if value == "--" {
			continue
		}
		if shellOptionTakesOperand(value) {
			i++
			continue
		}
		if shellOptionRunsCommand(value) {
			if i+1 < len(tokens) {
				return tokens[i+1].Value, true
			}
			return "", false
		}
		if strings.HasPrefix(value, "-") {
			continue
		}
		return "", false
	}
	return "", false
}

func shellOptionTakesOperand(value string) bool {
	switch value {
	case "-o", "-O", "--option", "--shopt", "--rcfile", "--init-file":
		return true
	default:
		return false
	}
}

func shellOptionRunsCommand(value string) bool {
	if value == "-c" {
		return true
	}
	return strings.HasPrefix(value, "-") && !strings.HasPrefix(value, "--") && strings.Contains(value[1:], "c")
}

func envSplitString(tokens []token) (string, bool) {
	for len(tokens) > 0 {
		value := tokens[0].Value
		if isEnvAssignment(tokens[0]) {
			tokens = tokens[1:]
			continue
		}
		if value == "--" {
			return "", false
		}
		if value == "-S" || value == "--split-string" {
			if len(tokens) > 1 {
				return tokens[1].Value, true
			}
			return "", false
		}
		if strings.HasPrefix(value, "--split-string=") {
			return strings.TrimPrefix(value, "--split-string="), true
		}
		if !strings.HasPrefix(value, "-") {
			return "", false
		}
		if envOptionTakesOperand(value) {
			tokens = tokens[1:]
			if len(tokens) > 0 {
				tokens = tokens[1:]
			}
			continue
		}
		tokens = tokens[1:]
	}
	return "", false
}

func commandSubstitutionScripts(segment string) []string {
	var scripts []string
	var quote rune
	escaped := false
	for i := 0; i < len(segment); i++ {
		if escaped {
			escaped = false
			continue
		}
		if segment[i] == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if rune(segment[i]) == quote {
				quote = 0
			}
			continue
		}
		if segment[i] == '\'' || segment[i] == '"' {
			quote = rune(segment[i])
			continue
		}
		if i+1 < len(segment) && segment[i] == '$' && segment[i+1] == '(' {
			if script, end, ok := dollarCommandSubstitution(segment, i+2); ok {
				scripts = append(scripts, script)
				i = end
			}
			continue
		}
		if segment[i] == '`' {
			if script, end, ok := backtickCommandSubstitution(segment, i+1); ok {
				scripts = append(scripts, script)
				i = end
			}
		}
	}
	return scripts
}

func dollarCommandSubstitution(value string, start int) (string, int, bool) {
	depth := 1
	for i := start; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return value[start:i], i, true
			}
		}
	}
	return "", 0, false
}

func backtickCommandSubstitution(value string, start int) (string, int, bool) {
	for i := start; i < len(value); i++ {
		if value[i] == '`' {
			return value[start:i], i, true
		}
	}
	return "", 0, false
}

func splitSegments(command string) []string {
	var segments []string
	var b strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if strings.TrimSpace(b.String()) != "" {
			segments = append(segments, b.String())
		}
		b.Reset()
	}

	for i := 0; i < len(command); i++ {
		r := rune(command[i])
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			b.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			b.WriteRune(r)
			quote = r
			continue
		}
		if r == '|' && i > 0 && command[i-1] == '>' {
			b.WriteRune(r)
			continue
		}
		if r == ';' || r == '|' || r == '\n' || r == '\r' {
			flush()
			continue
		}
		if r == '&' && i+1 < len(command) && command[i+1] == '&' {
			flush()
			i++
			continue
		}
		if r == '&' {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return segments
}

func shellFields(segment string) []token {
	var tokens []token
	var b strings.Builder
	var quote rune
	quoted := false
	escaped := false
	skipClobberPipe := false

	flush := func() {
		if b.Len() == 0 && !quoted {
			return
		}
		tokens = append(tokens, token{Value: b.String(), Quoted: quoted})
		b.Reset()
		quoted = false
	}

	for i, r := range segment {
		if skipClobberPipe {
			skipClobberPipe = false
			if r == '|' {
				continue
			}
		}
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			quoted = true
			continue
		}
		if r == '>' {
			if b.Len() > 0 && isDigits(b.String()) && !quoted {
				b.WriteRune(r)
				if i+1 < len(segment) && segment[i+1] == '|' {
					skipClobberPipe = true
					continue
				}
				flush()
				continue
			}
			flush()
			value := ">"
			if i+1 < len(segment) && segment[i+1] == '|' {
				value = ">|"
				skipClobberPipe = true
			}
			tokens = append(tokens, token{Value: value})
			continue
		}
		if r == '|' && b.String() == ">" && !quoted {
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return tokens
}

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
	// /dev/null, /dev/zero, /dev/random, /dev/urandom are harmless sinks
	switch path {
	case "/dev/null", "/dev/zero", "/dev/random", "/dev/urandom",
		"/dev/stdin", "/dev/stdout", "/dev/stderr", "/dev/fd":
		return false
	}
	for _, prefix := range []string{"/etc", "/var", "/home", "/root", "/usr", "/bin", "/sbin", "/lib", "/opt", "/srv", "/dev", "/boot", "/sys", "/proc"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func hasDangerousDDOutput(tokens []token) bool {
	for _, tok := range tokens {
		if !strings.HasPrefix(tok.Value, "of=") {
			continue
		}
		output := strings.TrimPrefix(tok.Value, "of=")
		if strings.HasPrefix(output, "/dev/") || strings.HasPrefix(output, "/") {
			return true
		}
		for _, dev := range []string{"/dev/sd", "/dev/nvme", "/dev/hd", "/dev/xvd", "/dev/vd", "/dev/mmcblk", "/dev/md", "/dev/dm-", "/dev/loop", "/dev/disk"} {
			if strings.HasPrefix(output, dev) {
				return true
			}
		}
	}
	return false
}

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
