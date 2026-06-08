package capabilities

import "strings"

type Capabilities struct {
	OK                    bool   `json:"ok"`
	OS                    string `json:"os"`
	TmuxInstalled         bool   `json:"tmux_installed"`
	PackageManager        string `json:"package_manager,omitempty"`
	NonInteractiveInstall bool   `json:"non_interactive_install"`
	SessionBackend        string `json:"session_backend"`
}

func ProbeCommand() string {
	return strings.Join([]string{
		"stty -echo 2>/dev/null; printf '__ASSH_PROBE__\\n'",
		"os=$(uname -s 2>/dev/null | tr '[:upper:]' '[:lower:]')",
		"case \"$os\" in linux*) os=linux ;; darwin*) os=darwin ;; *) os=${os:-unknown} ;; esac",
		"printf 'os=%s\\n' \"$os\"",
		"if command -v tmux >/dev/null 2>&1; then printf 'tmux=installed\\n'; else printf 'tmux=missing\\n'; fi",
		"pkg=unknown",
		"for candidate in apt dnf yum apk pacman brew; do if command -v \"$candidate\" >/dev/null 2>&1; then pkg=$candidate; break; fi; done",
		"printf 'pkg=%s\\n' \"$pkg\"",
		"if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then printf 'install=noninteractive\\n'; else printf 'install=unknown\\n'; fi",
		"printf '\\n__ASSH_PROBE_END__\\n'",
	}, "; ")
}

func ParseProbe(raw []byte) Capabilities {
	text := string(raw)
	// Extract content between probe markers with newline suffix to distinguish
	// actual output from PTY-echoed command text (echoed has \n as literal chars).
	s := strings.Index(text, "__ASSH_PROBE__\n")
	if s >= 0 {
		s += len("__ASSH_PROBE__\n")
	} else {
		// Fallback: no newline suffix (no-PTY mode)
		s = strings.Index(text, "__ASSH_PROBE__")
		if s >= 0 {
			s += len("__ASSH_PROBE__")
		} else {
			s = 0
		}
	}
	e := strings.Index(text[s:], "\n__ASSH_PROBE_END__")
	if e < 0 {
		e = strings.Index(text[s:], "__ASSH_PROBE_END__")
	}
	if e >= 0 {
		e += s
	} else {
		e = len(text)
		s = 0
	}

	content := text[s:e]
	values := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			values[key] = value
		}
	}

	osName := values["os"]
	return Capabilities{
		OK:                    true,
		OS:                    osName,
		TmuxInstalled:         values["tmux"] == "installed",
		PackageManager:        packageManager(values["pkg"]),
		NonInteractiveInstall: values["install"] == "noninteractive",
		SessionBackend:        sessionBackend(osName),
	}
}

func packageManager(value string) string {
	if value == "unknown" {
		return ""
	}
	return value
}

func sessionBackend(osName string) string {
	if osName == "linux" || osName == "darwin" {
		return "tmux"
	}
	return "unsupported"
}
