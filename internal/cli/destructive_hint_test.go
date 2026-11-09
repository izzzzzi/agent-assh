package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/session"
)

func TestDestructiveExecHintUsesMatchingRegisteredSID(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	entry, err := session.LoadRegistry(stateBaseDir(), "abcdef12")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	entry.Host = "example.com"
	entry.User = "root"
	entry.Port = 22
	entry.Identity = ""
	if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	got := executeTransferJSONError(t, []string{"exec", "--host", "example.com", "--", "rm", "-rf", "/tmp/x"})
	hint, _ := got["hint"].(string)
	if !strings.Contains(hint, "assh session exec -s abcdef12 --confirm-danger") {
		t.Fatalf("hint=%q does not contain real SID", hint)
	}
	if strings.Contains(hint, "-s SID") {
		t.Fatalf("hint still contains placeholder: %q", hint)
	}
}

func TestDestructiveExecHintIgnoresMismatchedIdentity(t *testing.T) {
	writeTestSessionRegistry(t, "abcdef12")
	entry, err := session.LoadRegistry(stateBaseDir(), "abcdef12")
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	entry.Host = "example.com"
	entry.Identity = "/keys/other"
	entry.CreatedAt = time.Now().UTC()
	if err := session.SaveRegistry(stateBaseDir(), entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	args := []string{"exec", "--host", "example.com", "--identity", "/keys/current",
		"--", "rm", "-rf", "/tmp/x"}
	got := executeTransferJSONError(t, args)
	hint, _ := got["hint"].(string)
	if strings.Contains(hint, "abcdef12") {
		t.Fatalf("hint reused mismatched identity SID: %q", hint)
	}
	if !strings.Contains(hint, "-s SID") {
		t.Fatalf("hint lost safe placeholder fallback: %q", hint)
	}
}
