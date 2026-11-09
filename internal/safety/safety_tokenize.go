package safety

import "strings"

// Shell tokenizer for the safety classifier.
//
// This file turns a raw command string into segments (split on ;, |, &&, \n)
// and then into tokens (split on whitespace, honoring quotes and escapes).
// It also extracts command-substitution scripts ($(...), `...`) so nested
// commands inside env/sh -c wrappers can be classified recursively.
//
// The output feeds checkSegment in safety.go, which inspects the tokens
// against the rule detectors in safety_rules.go.

func commandSubstitutionScripts(segment string) []string {
	var scripts []string
	var quote byte
	escaped := false
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		// Single quotes make everything literal, including backslashes.
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			if quote == '"' {
				quote = 0
			} else {
				quote = '"'
			}
			continue
		}
		if c == '\'' && quote == 0 {
			quote = '\''
			continue
		}
		// The shell still expands $(...) and backticks inside double quotes, so
		// they are inspected there as well as unquoted. Process substitution,
		// <(...) and >(...), runs a command too, but only unquoted.
		opensSubstitution := c == '$' || (quote == 0 && (c == '<' || c == '>'))
		if i+1 < len(segment) && opensSubstitution && segment[i+1] == '(' {
			if script, end, ok := dollarCommandSubstitution(segment, i+2); ok {
				scripts = append(scripts, script)
				i = end
			}
			continue
		}
		if c == '`' {
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

// splitSegments breaks a compound command into segments on ;, |, &&, and
// newlines. Quoted versions of these characters are preserved. The clobber
// redirect >| is kept intact (the leading > is not treated as a pipe boundary).
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
		// Inside single quotes nothing is special, backslash included. Checking
		// the escape first would keep the quote "open" past `'a\'` and hide
		// everything after it from the classifier.
		if quote == '\'' {
			b.WriteRune(r)
			if r == '\'' {
				quote = 0
			}
			continue
		}
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

// shellFields tokenizes a single command segment into words, honoring single
// and double quotes, backslash escapes, and the > / >| redirect operators.
// The redirect operators are emitted as their own tokens so rule detectors
// can pair them with their targets unambiguously.
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
		if quote == '\'' {
			if r == '\'' {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
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
			if handleRedirect(segment, i, &b, &tokens, quoted, &skipClobberPipe, flush) {
				continue
			}
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

// handleRedirect processes a '>' rune in a shell segment. It emits either a
// file-descriptor-prefixed redirect (e.g. "2>") as part of the current token,
// or a standalone ">" / ">|" token. Returns true if the rune was consumed.
func handleRedirect(
	segment string, i int, b *strings.Builder, tokens *[]token,
	quoted bool, skipClobberPipe *bool, flush func(),
) bool {
	if b.Len() > 0 && isDigits(b.String()) && !quoted {
		b.WriteByte('>')
		if i+1 < len(segment) && segment[i+1] == '|' {
			*skipClobberPipe = true
			return true
		}
		flush()
		return true
	}
	flush()
	value := ">"
	if i+1 < len(segment) && segment[i+1] == '|' {
		value = ">|"
		*skipClobberPipe = true
	}
	*tokens = append(*tokens, token{Value: value})
	return true
}
