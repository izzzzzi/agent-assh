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
		"os=$(uname -s 2>/dev/null | tr '[:upper:]' '[:lower:]')",
		"case \"$os\" in linux*) os=linux ;; darwin*) os=darwin ;; *) os=${os:-unknown} ;; esac",
		"printf 'os=%s\\n' \"$os\"",
		"if command -v tmux >/dev/null 2>&1; then printf 'tmux=installed\\n'; else printf 'tmux=missing\\n'; fi",
		"pkg=unknown",
		"for candidate in apt dnf yum apk pacman brew; do if command -v \"$candidate\" >/dev/null 2>&1; then pkg=$candidate; break; fi; done",
		"printf 'pkg=%s\\n' \"$pkg\"",
		"if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then printf 'install=noninteractive\\n'; else printf 'install=unknown\\n'; fi",
	}, "; ")
}

func ParseProbe(raw []byte) Capabilities {
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		values[key] = value
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
