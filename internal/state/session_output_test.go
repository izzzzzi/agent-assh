package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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

func TestSessionOutputStoreWriteStampsCachedAt(t *testing.T) {
	store := NewSessionOutputStore(t.TempDir())
	page := SessionOutputPage{
		SID:        "abcdef12",
		Seq:        1,
		Stream:     "stdout",
		Limit:      50,
		TotalLines: 1,
		Content:    "x\n",
	}

	if err := store.Write(page); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	listed, err := store.List("abcdef12")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("pages = %#v, want one page", listed)
	}
	if listed[0].CachedAt.IsZero() {
		t.Fatal("CachedAt = zero, want a write timestamp")
	}
}

func TestValidateSessionOutputPage(t *testing.T) {
	valid := SessionOutputPage{
		SID:        "abcdef12",
		Seq:        1,
		Stream:     "stdout",
		Offset:     0,
		Limit:      50,
		TotalLines: 10,
	}

	tests := []struct {
		name    string
		mutate  func(*SessionOutputPage)
		wantErr bool
	}{
		{"valid stdout page", func(*SessionOutputPage) {}, false},
		{"valid stderr page", func(p *SessionOutputPage) { p.Stream = "stderr" }, false},
		{"invalid session id", func(p *SessionOutputPage) { p.SID = "../bad" }, true},
		{"zero seq", func(p *SessionOutputPage) { p.Seq = 0 }, true},
		{"negative seq", func(p *SessionOutputPage) { p.Seq = -1 }, true},
		{"invalid stream", func(p *SessionOutputPage) { p.Stream = "combined" }, true},
		{"negative offset", func(p *SessionOutputPage) { p.Offset = -1 }, true},
		{"zero limit", func(p *SessionOutputPage) { p.Limit = 0 }, true},
		{"negative total lines", func(p *SessionOutputPage) { p.TotalLines = -1 }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := valid
			tt.mutate(&page)
			err := validateSessionOutputPage(page)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSessionOutputPage(%#v) error = %v, wantErr %v", page, err, tt.wantErr)
			}
		})
	}
}

func TestSessionOutputStoreWriteRejectsInvalidPage(t *testing.T) {
	store := NewSessionOutputStore(t.TempDir())

	page := SessionOutputPage{SID: "abcdef12", Seq: 0, Stream: "stdout", Limit: 1}
	if err := store.Write(page); err == nil {
		t.Fatal("Write() error = nil, want a validation error")
	}
}

func TestSessionOutputStoreWriteReportsDirectoryCreationFailure(t *testing.T) {
	store := NewSessionOutputStore(filepath.Join(blockingFilePath(t), "session_outputs"))
	page := SessionOutputPage{
		SID:        "abcdef12",
		Seq:        1,
		Stream:     "stdout",
		Limit:      50,
		TotalLines: 1,
		Content:    "x\n",
	}

	if err := store.Write(page); err == nil {
		t.Fatal("Write() error = nil, want a directory creation error")
	}
}

func TestSessionOutputStoreListMissingDirectoryIsEmpty(t *testing.T) {
	store := NewSessionOutputStore(t.TempDir())

	listed, err := store.List("abcdef12")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("pages = %#v, want none", listed)
	}
}

func TestSessionOutputStoreListRejectsInvalidSID(t *testing.T) {
	store := NewSessionOutputStore(t.TempDir())

	if _, err := store.List("../escape"); err == nil {
		t.Fatal("List() error = nil, want invalid session id")
	}
}

func TestSessionOutputStoreListSkipsNoiseAndSorts(t *testing.T) {
	base := t.TempDir()
	store := NewSessionOutputStore(base)
	sid := "abcdef12"

	// Written out of order so the sort is actually exercised.
	pages := []SessionOutputPage{
		{SID: sid, Seq: 2, Stream: "stdout", Limit: 10, TotalLines: 5, Content: "second\n"},
		{SID: sid, Seq: 1, Stream: "stdout", Limit: 10, TotalLines: 5, Content: "first\n"},
	}
	for _, page := range pages {
		if err := store.Write(page); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	sidDir := filepath.Join(base, "session_outputs", sid)
	if err := os.WriteFile(filepath.Join(sidDir, "notes.txt"), []byte("ignore"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(sidDir, "nested"), 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	foreign := SessionOutputPage{SID: "deadbeef", Seq: 9, Stream: "stdout", Limit: 10, TotalLines: 1}
	body, err := json.Marshal(foreign)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sidDir, "seq-9-stdout-offset-0-limit-10.json"), body, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	listed, err := store.List(sid)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("pages = %#v, want only the two pages of this session", listed)
	}
	if listed[0].Content != "first\n" || listed[1].Content != "second\n" {
		t.Fatalf("pages are not sorted by seq: %#v", listed)
	}
}

func TestSessionOutputStoreListRejectsCorruptEntry(t *testing.T) {
	base := t.TempDir()
	store := NewSessionOutputStore(base)
	sid := "abcdef12"

	sidDir := filepath.Join(base, "session_outputs", sid)
	if err := os.MkdirAll(sidDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	name := "seq-1-stdout-offset-0-limit-1.json"
	if err := os.WriteFile(filepath.Join(sidDir, name), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := store.List(sid); err == nil {
		t.Fatal("List() error = nil, want a decode error")
	}
}

func TestSessionOutputStoreListRejectsInvalidStoredPage(t *testing.T) {
	base := t.TempDir()
	store := NewSessionOutputStore(base)
	sid := "abcdef12"

	// Well-formed JSON for this session, but with a seq that fails validation.
	stored := SessionOutputPage{SID: sid, Seq: 0, Stream: "stdout", Limit: 1}
	body, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	sidDir := filepath.Join(base, "session_outputs", sid)
	if err := os.MkdirAll(sidDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sidDir, "seq-0-stdout-offset-0-limit-1.json"), body, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := store.List(sid); err == nil {
		t.Fatal("List() error = nil, want a validation error")
	}
}
