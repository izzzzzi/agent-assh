package safety

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Policy is an additive, deny-only overlay on top of the built-in classifier.
// It can only ADD denied command names; it can never relax a built-in rule. The
// only audited bypass remains --confirm-danger. Rules are matched against the
// tokenizer's resolved command name, so they compose with sudo/env/sh -c and
// command-substitution unwrapping just like the built-in rules.
type Policy struct {
	deny   map[string]int // command name -> source line number
	path   string
	sha256 string
}

// PolicyError describes why a policy file was rejected. Loading fails closed: a
// present-but-invalid file is an error, never a silent no-op.
type PolicyError struct {
	Code    string // "safety_policy_invalid" | "safety_policy_parse_error"
	Message string
	Hint    string
}

func (e *PolicyError) Error() string { return e.Message }

// DefaultPolicyPath returns ~/.config/assh/safety.rules (respecting XDG_CONFIG_HOME).
func DefaultPolicyPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "assh", "safety.rules")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "assh", "safety.rules")
}

// LoadPolicy reads and validates a deny-only policy file. It returns (nil, nil)
// when the file is absent (default behavior is unchanged). A present file that is
// not a regular 0600 file owned by the current user, or that contains a malformed
// line, is rejected with a *PolicyError (fail closed).
func LoadPolicy(path string) (*Policy, error) {
	if path == "" {
		return nil, nil
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, &PolicyError{Code: "safety_policy_invalid", Message: path + ": " + err.Error()}
	}
	if !info.Mode().IsRegular() {
		return nil, &PolicyError{
			Code:    "safety_policy_invalid",
			Message: path + " is not a regular file",
			Hint:    "the safety policy must be a plain file owned by you with mode 0600",
		}
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, &PolicyError{
			Code:    "safety_policy_invalid",
			Message: fmt.Sprintf("%s has insecure permissions %#o (group/other access)", path, perm),
			Hint:    "chmod 600 " + path,
		}
	}
	if err := checkOwnership(path, info); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &PolicyError{Code: "safety_policy_invalid", Message: path + ": " + err.Error()}
	}

	policy := &Policy{deny: map[string]int{}, path: path}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, " \t") {
			return nil, &PolicyError{
				Code:    "safety_policy_parse_error",
				Message: fmt.Sprintf("%s:%d: rule must be a single command name, got %q", path, lineNum, line),
				Hint:    "one command name per line (e.g. `curl`); comments start with #",
			}
		}
		policy.deny[commandName(line)] = lineNum
	}
	if err := scanner.Err(); err != nil {
		return nil, &PolicyError{Code: "safety_policy_invalid", Message: path + ": " + err.Error()}
	}

	sum := sha256.Sum256(data)
	policy.sha256 = fmt.Sprintf("%x", sum[:])
	return policy, nil
}

// SHA256 returns the hex digest of the loaded policy file, for audit logging.
func (p *Policy) SHA256() string {
	if p == nil {
		return ""
	}
	return p.sha256
}

// Path returns the policy file path, for audit logging.
func (p *Policy) Path() string {
	if p == nil {
		return ""
	}
	return p.path
}

func (p *Policy) check(name string) Result {
	if p == nil {
		return Result{}
	}
	if line, ok := p.deny[name]; ok {
		return Result{
			Dangerous: true,
			Rule:      "policy_deny:" + name,
			Message:   fmt.Sprintf("matched destructive pattern: policy_deny:%s (source: policy:%d)", name, line),
		}
	}
	return Result{}
}
