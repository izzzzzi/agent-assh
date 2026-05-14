package session

import (
	"encoding/json"
	"strings"
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

func TestNewMetadata(t *testing.T) {
	before := time.Now().UTC()
	metadata := NewMetadata("abcdef12", "deploy", 2*time.Hour, "client-1")
	after := time.Now().UTC()

	if metadata.CreatedBy != "assh" {
		t.Fatalf("CreatedBy = %q, want %q", metadata.CreatedBy, "assh")
	}
	if metadata.SID != "abcdef12" {
		t.Fatalf("SID = %q, want %q", metadata.SID, "abcdef12")
	}
	if metadata.Label != "deploy" {
		t.Fatalf("Label = %q, want %q", metadata.Label, "deploy")
	}
	if metadata.TmuxName != "assh_abcdef12" {
		t.Fatalf("TmuxName = %q, want %q", metadata.TmuxName, "assh_abcdef12")
	}
	if metadata.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt location = %v, want UTC", metadata.CreatedAt.Location())
	}
	if metadata.CreatedAt.Before(before) || metadata.CreatedAt.After(after) {
		t.Fatalf("CreatedAt = %v, want between %v and %v", metadata.CreatedAt, before, after)
	}
	if metadata.TTLSeconds != 7200 {
		t.Fatalf("TTLSeconds = %d, want 7200", metadata.TTLSeconds)
	}
	if metadata.ClientID != "client-1" {
		t.Fatalf("ClientID = %q, want %q", metadata.ClientID, "client-1")
	}
}

func TestCanCleanupRequiresAsshMarker(t *testing.T) {
	good := Metadata{CreatedBy: "assh", SID: "abcdef12", TmuxName: "assh_abcdef12"}
	if !CanCleanup(good) {
		t.Fatalf("CanCleanup(good) = false, want true")
	}

	bad := good
	bad.CreatedBy = "other"
	if CanCleanup(bad) {
		t.Fatalf("CanCleanup(bad CreatedBy) = true, want false")
	}
}

func TestOpenRemoteCommandUsesDerivedSIDAndQuotedValues(t *testing.T) {
	metaJSON := `{"sid":"abcdef12","label":"don't/use"}`
	got := OpenRemoteCommand(metaJSON, "assh_abcdef12")

	for _, want := range []string{
		"mkdir -p ~/.assh/sessions",
		"mkdir -p ~/.assh/sessions/abcdef12",
		`printf %s '{"sid":"abcdef12","label":"don'"'"'t/use"}' > ~/.assh/sessions/abcdef12/meta.json`,
		"tmux new-session -d -s 'assh_abcdef12'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("OpenRemoteCommand() = %q, want to contain %q", got, want)
		}
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

func TestExpiredReturnsFalseWhenTTLNotPositive(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	for _, ttl := range []int64{0, -1} {
		metadata := Metadata{
			CreatedAt:  now.Add(-2 * time.Hour),
			TTLSeconds: ttl,
		}

		if metadata.Expired(now) {
			t.Fatalf("Expired() with TTLSeconds %d = true, want false", ttl)
		}
	}
}
