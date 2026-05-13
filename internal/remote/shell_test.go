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
	if !SafeSID("abcdef12") {
		t.Fatalf("SafeSID(%q) = false, want true", "abcdef12")
	}
	if SafeSID("../bad") {
		t.Fatalf("SafeSID(%q) = true, want false", "../bad")
	}
}
