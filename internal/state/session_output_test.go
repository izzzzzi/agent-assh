package state

import "testing"

func TestSessionOutputStoreWriteList(t *testing.T) {
	store := NewSessionOutputStore(t.TempDir())
	pages := []SessionOutputPage{
		{
			SID:        "abcdef12",
			Seq:        2,
			Stream:     "stdout",
			Offset:     10,
			Limit:      50,
			TotalLines: 12,
			Content:    "hello\n",
		},
		{
			SID:        "abcdef12",
			Seq:        2,
			Stream:     "stdout",
			Offset:     12,
			Limit:      25,
			TotalLines: 12,
			Content:    "world\n",
		},
	}
	for _, page := range pages {
		if err := store.Write(page); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	listed, err := store.List("abcdef12")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("pages = %#v, want two pages", listed)
	}
	if listed[0].Offset != 10 || listed[1].Offset != 12 {
		t.Fatalf("unexpected page order: %#v", listed)
	}
	if listed[0].Content != "hello\n" || listed[1].Content != "world\n" {
		t.Fatalf("unexpected page contents: %#v", listed)
	}
}
