package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func writePolicy(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "safety.rules")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPolicyAbsentIsNoOp(t *testing.T) {
	p, err := LoadPolicy(filepath.Join(t.TempDir(), "nope.rules"))
	if err != nil {
		t.Fatalf("absent policy should not error: %v", err)
	}
	if p != nil {
		t.Fatalf("absent policy should be nil")
	}
}

func TestPolicyAddsDenyRule(t *testing.T) {
	path := writePolicy(t, "# block curl\ncurl\nwget\n", 0o600)
	p, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if res := CheckCommandWithPolicy("curl http://evil", p); !res.Dangerous {
		t.Errorf("curl should be denied by policy")
	}
	if res := CheckCommandWithPolicy("ls -la", p); res.Dangerous {
		t.Errorf("ls must not be denied")
	}
}

func TestPolicyComposesWithUnwrap(t *testing.T) {
	path := writePolicy(t, "curl\n", 0o600)
	p, _ := LoadPolicy(path)
	// The tokenizer unwraps sudo / sh -c, so the policy name still matches.
	for _, cmd := range []string{
		"sudo curl http://x",
		"sh -c 'curl http://x'",
		"env FOO=bar curl http://x",
	} {
		if res := CheckCommandWithPolicy(cmd, p); !res.Dangerous {
			t.Errorf("policy should catch wrapped command: %q", cmd)
		}
	}
}

func TestPolicyCannotRelaxBuiltin(t *testing.T) {
	// An "allow"-looking line is just a (meaningless) command-name deny; it can
	// never relax the built-in rm -rf rule.
	path := writePolicy(t, "echo\n", 0o600)
	p, _ := LoadPolicy(path)
	if res := CheckCommandWithPolicy("rm -rf /", p); !res.Dangerous {
		t.Errorf("built-in rm -rf rule must still fire with policy present")
	}
}

func TestPolicyRejectsInsecurePerms(t *testing.T) {
	path := writePolicy(t, "curl\n", 0o644)
	_, err := LoadPolicy(path)
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("want PolicyError, got %v", err)
	}
	if pe.Code != "safety_policy_invalid" {
		t.Errorf("code = %q, want safety_policy_invalid", pe.Code)
	}
}

func TestPolicyRejectsMalformedLine(t *testing.T) {
	path := writePolicy(t, "rm -rf /\n", 0o600)
	_, err := LoadPolicy(path)
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("want PolicyError, got %v", err)
	}
	if pe.Code != "safety_policy_parse_error" {
		t.Errorf("code = %q, want safety_policy_parse_error", pe.Code)
	}
}

func TestPolicyHashRecorded(t *testing.T) {
	path := writePolicy(t, "curl\n", 0o600)
	p, _ := LoadPolicy(path)
	if p.SHA256() == "" {
		t.Errorf("expected non-empty policy hash")
	}
	if p.Path() != path {
		t.Errorf("path = %q, want %q", p.Path(), path)
	}
}

func TestNilPolicyMatchesBuiltin(t *testing.T) {
	if res := CheckCommandWithPolicy("rm -rf /", nil); !res.Dangerous {
		t.Errorf("nil policy should still apply built-in rules")
	}
	if res := CheckCommandWithPolicy("ls", nil); res.Dangerous {
		t.Errorf("nil policy should not flag safe command")
	}
}
