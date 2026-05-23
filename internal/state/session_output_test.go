package state

import "testing"

func TestSessionOutputStoreWriteList(t *testing.T) {
	store := NewSessionOutputStore(t.TempDir())
	page := SessionOutputPage{
		SID:        "abcdef12",
		Seq:        2,
		Stream:     "stdout",
		Offset:     10,
		Limit:      50,
		TotalLines: 12,
		Content:    "hello\n",
	}

	if err := store.Write(page); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	pages, err := store.List("abcdef12")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages = %#v, want one page", pages)
	}
	if pages[0].SID != "abcdef12" || pages[0].Seq != 2 || pages[0].Stream != "stdout" || pages[0].Content != "hello\n" {
		t.Fatalf("unexpected page: %#v", pages[0])
	}
}
