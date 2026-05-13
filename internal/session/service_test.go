package session

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMetadataJSON(t *testing.T) {
	metadata := Metadata{
		CreatedBy: "assh",
		SID:       "abcdef12",
		Label:     "deploy",
		TmuxName:  "assh_abcdef12",
		CreatedAt: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got Metadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.CreatedBy != "assh" {
		t.Fatalf("CreatedBy = %q, want %q", got.CreatedBy, "assh")
	}
	if got.TmuxName != "assh_abcdef12" {
		t.Fatalf("TmuxName = %q, want %q", got.TmuxName, "assh_abcdef12")
	}
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	metadata := Metadata{
		CreatedAt:  now.Add(-2 * time.Hour),
		TTLSeconds: 3600,
	}

	if !metadata.Expired(now) {
		t.Fatalf("Expired() = false, want true")
	}
}
