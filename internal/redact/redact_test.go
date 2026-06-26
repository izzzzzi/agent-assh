package redact

import (
	"strings"
	"testing"
)

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func TestRedactPatterns(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantCount int
		mustHave  string
		mustNot   string
	}{
		{"aws_key", "key=AKIAIOSFODNN7EXAMPLE done", 1, "[REDACTED:aws_key]", "AKIAIOSFODNN7EXAMPLE"},
		{"jwt", "tok eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc12345defg done", 1, "[REDACTED:jwt]", "eyJhbGci"},
		{"bearer", "Authorization: Bearer abcdef0123456789ABCDEF", 1, "[REDACTED:bearer]", "abcdef0123456789"},
		{"password assignment", `password = "hunter2secret"`, 1, "[REDACTED:secret]", "hunter2secret"},
		{"token kv", "token=supersecretvalue123", 1, "[REDACTED:secret]", "supersecretvalue123"},
		{"api_key colon", "api_key: 'abcd1234efgh'", 1, "[REDACTED:secret]", "abcd1234efgh"},
		{"preserves key name", "password=hunter2", 1, "password=", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, res := String(tc.in)
			if res.Count != tc.wantCount {
				t.Fatalf("count = %d, want %d (out=%q)", res.Count, tc.wantCount, out)
			}
			if tc.mustHave != "" && !strings.Contains(out, tc.mustHave) {
				t.Errorf("output %q missing %q", out, tc.mustHave)
			}
			if tc.mustNot != "" && strings.Contains(out, tc.mustNot) {
				t.Errorf("output %q still contains secret %q", out, tc.mustNot)
			}
		})
	}
}

func TestRedactFalsePositives(t *testing.T) {
	// Things that look secret-ish but must not be mangled.
	clean := []string{
		"commit 9f8e7d6c5b4a3210fedcba9876543210deadbeef",
		"base64 payload: aGVsbG8gd29ybGQgdGhpcyBpcyBmaW5l",
		"see https://example.com/path?token",    // bare key, no value after delimiter
		"the password policy requires rotation", // no assignment delimiter
	}
	for _, in := range clean {
		out, res := String(in)
		if res.Count != 0 {
			t.Errorf("false positive on %q -> %q (count %d)", in, out, res.Count)
		}
		if out != in {
			t.Errorf("mutated clean input %q -> %q", in, out)
		}
	}
}

func TestRedactPreservesLineCount(t *testing.T) {
	in := "line1\npassword=secret123\nline3\nAKIAIOSFODNN7EXAMPLE\nline5\n"
	out, _ := String(in)
	if got, want := lineCount(out), lineCount(in); got != want {
		t.Fatalf("line count changed: got %d want %d\n%q", got, want, out)
	}
}

func TestRedactPEMBlock(t *testing.T) {
	in := strings.Join([]string{
		"prefix line",
		"-----BEGIN RSA PRIVATE KEY-----",
		"MIIEowIBAAKCAQEAabcdef",
		"ghijklmnopqrstuvwxyz12",
		"-----END RSA PRIVATE KEY-----",
		"suffix line",
		"",
	}, "\n")
	out, res := String(in)
	if res.Count != 1 {
		t.Fatalf("PEM block count = %d, want 1", res.Count)
	}
	if lineCount(out) != lineCount(in) {
		t.Fatalf("PEM line count changed: %d vs %d", lineCount(out), lineCount(in))
	}
	if strings.Contains(out, "MIIEowIBAAKCAQEA") {
		t.Errorf("PEM body leaked: %q", out)
	}
	if !strings.HasPrefix(out, "prefix line\n") || !strings.Contains(out, "suffix line") {
		t.Errorf("non-secret lines damaged: %q", out)
	}
}

func TestRedactUnterminatedPEM(t *testing.T) {
	in := "-----BEGIN OPENSSH PRIVATE KEY-----\nbodybodybody\nmorebody\n"
	out, res := String(in)
	if res.Count != 1 {
		t.Fatalf("count = %d, want 1", res.Count)
	}
	if strings.Contains(out, "bodybody") {
		t.Errorf("unterminated PEM body leaked: %q", out)
	}
	if lineCount(out) != lineCount(in) {
		t.Errorf("line count changed: %d vs %d", lineCount(out), lineCount(in))
	}
}

func TestRedactEmpty(t *testing.T) {
	out, res := String("")
	if out != "" || res.Count != 0 {
		t.Fatalf("empty input mishandled: %q %d", out, res.Count)
	}
	b, r := Bytes(nil)
	if b != nil || r.Count != 0 {
		t.Fatalf("nil bytes mishandled")
	}
}
