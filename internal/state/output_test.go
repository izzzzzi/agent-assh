package state

import "testing"

func TestOutputStoreWriteAndReadPage(t *testing.T) {
	store := NewOutputStore(t.TempDir())

	if err := store.Write("abcdef12", []byte("a\nb\nc\n"), []byte("err\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	page, err := store.Read("abcdef12", "stdout", 1, 1)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if page.Content != "b\n" {
		t.Fatalf("Content = %q, want %q", page.Content, "b\n")
	}
	if page.TotalLines != 3 {
		t.Fatalf("TotalLines = %d, want 3", page.TotalLines)
	}
	if !page.HasMore {
		t.Fatalf("HasMore = false, want true")
	}
}

func TestOutputStoreRejectsBadID(t *testing.T) {
	store := NewOutputStore(t.TempDir())

	if _, err := store.Read("../bad", "stdout", 0, 1); err == nil {
		t.Fatalf("Read() error = nil, want error")
	}
}
