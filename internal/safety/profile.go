package safety

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Profile defines an allow-list of command patterns.
type Profile struct {
	Allow []string `json:"allow"`
}

// Profiles is a map of named profiles loaded from JSON.
type Profiles struct {
	Profile map[string]Profile `json:"profile"`
}

// DefaultProfilePath returns ~/.config/assh/profiles.json (respects XDG_CONFIG_HOME).
func DefaultProfilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
		if dir == "" {
			dir = "/tmp"
		}
		dir = filepath.Join(dir, ".config")
	}
	return filepath.Join(dir, "assh", "profiles.json")
}

// LoadProfiles reads and parses a profiles JSON file. Returns (nil, nil) if
// the file does not exist.
func LoadProfiles(path string) (*Profiles, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p Profiles
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	return &p, nil
}

// matchPattern checks if tokenized command matches a profile allow pattern.
//
// Wildcards:
//   - "*" as a whole token matches any remaining tokens
//   - "*" within a path component matches any chars (e.g. "/var/log/*" matches "/var/log/syslog")
//
// Examples:
//
//	pattern "journalctl *"         matches ["journalctl", "-u", "nginx"]
//	pattern "df -h"                matches ["df", "-h"]
//	pattern "cat /var/log/*"       matches ["cat", "/var/log/syslog"]
//	pattern "*"                    matches everything
func matchPattern(pattern string, args []string) bool {
	parts := strings.Fields(pattern)
	if len(parts) == 0 || len(args) == 0 {
		return false
	}
	// command name must match exactly
	if !wildcardMatch(parts[0], args[0]) {
		return false
	}
	pi, ai := 1, 1
	for pi < len(parts) && ai < len(args) {
		if parts[pi] == "*" {
			return true
		}
		if !wildcardMatch(parts[pi], args[ai]) {
			return false
		}
		pi++
		ai++
	}
	// pattern has more parts — must be trailing wildcard
	if pi < len(parts) {
		return parts[pi] == "*"
	}
	// all pattern parts matched and no extra args
	return ai >= len(args)
}

// wildcardMatch checks if s matches pattern, where * matches any chars.
func wildcardMatch(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	// simple glob: split on *, match prefix/suffix/
	parts := strings.SplitN(pattern, "*", 2)
	if parts[0] != "" && !strings.HasPrefix(s, parts[0]) {
		return false
	}
	if parts[1] != "" && !strings.HasSuffix(s, parts[1]) {
		return false
	}
	return true
}

// Match checks whether command is allowed by the named profile.
// Returns non-dangerous Result if allowed, dangerous Result if denied or profile not found.
func (p *Profiles) Match(profileName string, command string) Result {
	command = strings.TrimSpace(command)
	if command == "" {
		return Result{Dangerous: true, Rule: "profile", Message: "empty command"}
	}
	profile, ok := p.Profile[profileName]
	if !ok {
		return Result{
			Dangerous: true,
			Rule:      "profile:not_found",
			Message:   fmt.Sprintf("profile '%s' not found", profileName),
		}
	}
	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return Result{Dangerous: true, Rule: "profile", Message: "empty command"}
	}
	for _, pattern := range profile.Allow {
		if pattern == "*" {
			return Result{Dangerous: false}
		}
		if matchPattern(pattern, tokens) {
			return Result{Dangerous: false}
		}
	}
	return Result{
		Dangerous: true,
		Rule:      "profile:" + profileName,
		Message:   fmt.Sprintf("'%s' is not in profile '%s'", command, profileName),
	}
}
