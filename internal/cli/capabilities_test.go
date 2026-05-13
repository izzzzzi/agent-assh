package cli

import "testing"

func TestCapabilitiesMissingHostReturnsJSONError(t *testing.T) {
	got := executeJSONError(t, []string{"capabilities"})
	if got["error"] != "invalid_args" || got["message"] != "host required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}
