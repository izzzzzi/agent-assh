package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/izzzzzi/agent-assh/internal/transport"
)

func TestParseTransferRead(t *testing.T) {
	cases := []struct {
		name       string
		stdout     string
		wantStatus string
		wantBody   string
	}{
		{"ok", transferReadMarker + "ok\nhello\nworld\n", "ok", "hello\nworld\n"},
		{"notfound", transferReadMarker + "notfound\n", "notfound", ""},
		{"dir", transferReadMarker + "dir\n", "dir", ""},
		{"toolarge", transferReadMarker + "toolarge:9999\n", "toolarge:9999", ""},
		{"no marker", "raw output\n", "", "raw output\n"},
		{"crlf marker", transferReadMarker + "ok\r\nbody", "ok", "body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := parseTransferRead([]byte(tc.stdout))
			if status != tc.wantStatus {
				t.Errorf("status = %q, want %q", status, tc.wantStatus)
			}
			if body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}

func TestRemoteFileReadCommandQuotesPath(t *testing.T) {
	cmd := remoteFileReadCommand("/etc/secret file", 4096)
	if !strings.Contains(cmd, "'/etc/secret file'") {
		t.Errorf("path not quoted: %q", cmd)
	}
	if !strings.Contains(cmd, "4096") {
		t.Errorf("limit not present: %q", cmd)
	}
}

func TestTransferReadOKRedactsAndStores(t *testing.T) {
	t.Setenv("ASSH_STATE_DIR", t.TempDir())
	oldRunSSH := runSSH
	t.Cleanup(func() { runSSH = oldRunSSH })
	runSSH = func(_ context.Context, _ transport.SSHCommand, _ string) transport.Result {
		return transport.Result{
			ExitCode: 0,
			Stdout:   []byte(transferReadMarker + "ok\nuser=bob\npassword=hunter2secret\n"),
		}
	}

	got := executeTransferJSON(t, []string{"transfer", "read", "--host", "h", "--user", "root", "--path", "/etc/app.conf"})
	if got["ok"] != true {
		t.Fatalf("unexpected: %#v", got)
	}
	if got["redacted"] != true {
		t.Errorf("expected redacted=true, got %#v", got["redacted"])
	}
	if _, ok := got["output_id"].(string); !ok {
		t.Errorf("missing output_id: %#v", got)
	}
}

func TestTransferReadTypedErrors(t *testing.T) {
	cases := []struct {
		status  string
		errCode string
	}{
		{"notfound", "remote_file_not_found"},
		{"dir", "not_a_file"},
		{"noperm", "permission_denied"},
		{"binary", "binary_file"},
		{"toolarge:5000", "file_too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			t.Setenv("ASSH_STATE_DIR", t.TempDir())
			oldRunSSH := runSSH
			t.Cleanup(func() { runSSH = oldRunSSH })
			runSSH = func(_ context.Context, _ transport.SSHCommand, _ string) transport.Result {
				return transport.Result{ExitCode: 0, Stdout: []byte(transferReadMarker + tc.status + "\n")}
			}
			got := executeTransferJSONError(t, []string{"transfer", "read", "--host", "h", "--user", "root", "--path", "/p"})
			if got["error"] != tc.errCode {
				t.Errorf("error = %v, want %s", got["error"], tc.errCode)
			}
			if got["hint"] == "" || got["hint"] == nil {
				t.Errorf("expected non-empty hint for %s", tc.errCode)
			}
		})
	}
}

func TestTransferReadRequiresPath(t *testing.T) {
	got := executeTransferJSONError(t, []string{"transfer", "read", "--host", "h", "--user", "root"})
	if got["error"] != "invalid_args" {
		t.Errorf("error = %v, want invalid_args", got["error"])
	}
}
