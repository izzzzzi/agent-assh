package ids

import "testing"

func TestNewIDIsValid(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !Valid(id) {
		t.Fatalf("Valid(%q) = false, want true", id)
	}
}

func TestValidRejectsUnsafeInput(t *testing.T) {
	for _, input := range []string{"", "../x", "abc/def", "abc def", "abc;rm", "abc\n"} {
		if Valid(input) {
			t.Fatalf("Valid(%q) = true, want false", input)
		}
	}
}
