package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestForwardStoreSaveLoadListDelete(t *testing.T) {
	store := NewForwardStore(t.TempDir())
	created := time.Now().UTC().Truncate(time.Second)
	record := ForwardRecord{
		Name:             "deploy",
		Host:             "example.com",
		User:             "root",
		Port:             22,
		Identity:         "key",
		Jump:             "bastion.example.com",
		Local:            []string{"127.0.0.1:8080:127.0.0.1:80"},
		Remote:           []string{"9000:127.0.0.1:9000"},
		Dynamic:          []string{"127.0.0.1:1080"},
		ControlSocket:    filepath.Join(t.TempDir(), "deploy.sock"),
		CreatedAtRFC3339: created.Format(time.RFC3339),
		PersistSeconds:   3600,
	}

	if err := store.Save(record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load("deploy")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Name != record.Name || loaded.Host != record.Host || loaded.Jump != record.Jump || loaded.ControlSocket != record.ControlSocket {
		t.Fatalf("loaded record = %#v, want %#v", loaded, record)
	}
	if len(loaded.Local) != 1 || loaded.Local[0] != record.Local[0] || len(loaded.Dynamic) != 1 {
		t.Fatalf("loaded rules = %#v", loaded)
	}

	records, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].Name != "deploy" {
		t.Fatalf("records = %#v", records)
	}

	if err := store.Delete("deploy"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Load("deploy"); err == nil {
		t.Fatal("Load() error = nil after delete")
	}
}

func TestForwardStoreRejectsUnsafeNames(t *testing.T) {
	store := NewForwardStore(t.TempDir())
	if err := store.Save(ForwardRecord{Name: "../bad"}); err == nil {
		t.Fatal("Save() error = nil, want invalid name")
	}
	if _, err := store.Load("../bad"); err == nil {
		t.Fatal("Load() error = nil, want invalid name")
	}
}
