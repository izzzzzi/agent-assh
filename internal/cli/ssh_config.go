package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type sshConfigEntry struct {
	Host         string
	HostName     string
	User         string
	Port         string
	IdentityFile string
	ProxyJump    string
}

func resolveSSHConfig(alias string) (sshConfigEntry, bool) {
	if alias == "" {
		return sshConfigEntry{}, false
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return sshConfigEntry{}, false
	}

	// ssh -G resolves the effective config for an alias, including all
	// inherited options from global config and wildcard hosts.
	output, err := exec.Command("ssh", "-G", alias).Output()
	if err != nil {
		// Try with identity file check
		return sshConfigEntry{}, false
	}

	entry := sshConfigEntry{Host: alias}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "hostname":
			if value != alias {
				entry.HostName = value
			}
		case "user":
			entry.User = value
		case "port":
			if value != "22" {
				entry.Port = value
			}
		case "identityfile":
			// ssh -G returns the full path. Expand ~ if present.
			if strings.HasPrefix(value, "~") {
				value = filepath.Join(home, value[1:])
			}
			entry.IdentityFile = value
		case "proxyjump":
			if value != "none" {
				entry.ProxyJump = value
			}
		}
	}

	// If hostname is different, use it; otherwise alias is the hostname.
	if entry.HostName != "" {
		entry.Host = entry.HostName
	}

	if entry.Host == "" && entry.HostName == "" {
		return sshConfigEntry{}, false
	}

	return entry, true
}

func applySSHConfigEntry(opts *sshOptions, entry sshConfigEntry) {
	if entry.HostName != "" {
		opts.Host = entry.HostName
	} else if entry.Host != "" {
		opts.Host = entry.Host
	}
	if entry.User != "" {
		opts.User = entry.User
	}
	if entry.Port != "" {
		// Port is already validated as int in the caller
	}
	if entry.IdentityFile != "" {
		opts.Identity = entry.IdentityFile
	}
	if entry.ProxyJump != "" {
		opts.Jump = entry.ProxyJump
	}
}
