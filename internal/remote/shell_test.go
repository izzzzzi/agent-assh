package remote

import "testing"

func TestSingleQuote(t *testing.T) {
	got := SingleQuote("a'b")
	want := `'a'"'"'b'`
	if got != want {
		t.Fatalf("SingleQuote() = %q, want %q", got, want)
	}
}

func TestSafeSID(t *testing.T) {
	for _, sid := range []string{
		"abcdef12",
		"abcdef1234567890abcdef1234567890",
	} {
		if !SafeSID(sid) {
			t.Fatalf("SafeSID(%q) = false, want true", sid)
		}
	}

	for _, sid := range []string{
		"abcdef1",
		"abcdef1234567890abcdef12345678901",
		"ABCDEF12",
		"abcdeg12",
		"../bad",
	} {
		if SafeSID(sid) {
			t.Fatalf("SafeSID(%q) = true, want false", sid)
		}
	}
}
