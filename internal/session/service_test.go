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

func TestCanCleanupRequiresAsshMarkerAndSafeSID(t *testing.T) {
	good := Metadata{CreatedBy: "assh", SID: "abcdef12", TmuxName: "assh_abcdef12"}
	if !CanCleanup(good) {
		t.Fatalf("CanCleanup(good) = false, want true")
	}

	bad := good
	bad.CreatedBy = "other"
	if CanCleanup(bad) {
		t.Fatalf("CanCleanup(bad CreatedBy) = true, want false")
	}

	bad = good
	bad.SID = "../bad"
	bad.TmuxName = "assh_../bad"
	if CanCleanup(bad) {
		t.Fatalf("CanCleanup(bad unsafe SID) = true, want false")
	}
}

func TestOpenRemoteCommandUsesValidatedSIDAndQuotedValues(t *testing.T) {
	metaJSON := `{"sid":"abcdef12","label":"don't/use"}`
	got, err := OpenRemoteCommand(metaJSON, "assh_abcdef12")
	if err != nil {
		t.Fatalf("OpenRemoteCommand() error = %v", err)
	}

	for _, want := range []string{
		`mkdir -p ~/.assh/sessions`,
		`mkdir -p ~/.assh/sessions/abcdef12`,
		`printf %s '{"sid":"abcdef12","label":"don'"'"'t/use"}' > ~/.assh/sessions/abcdef12/meta.json`,
		"tmux new-session -d -s 'assh_abcdef12'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("OpenRemoteCommand() = %q, want to contain %q", got, want)
		}
	}
}

func TestOpenRemoteCommandChecksTmuxBeforeCreatingSession(t *testing.T) {
	meta := NewMetadata("abcdef12", "deploy", time.Hour, "")
	body, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	cmd, err := OpenRemoteCommand(string(body), meta.TmuxName)
	if err != nil {
		t.Fatalf("OpenRemoteCommand() error = %v", err)
	}
	if !strings.Contains(cmd, "command -v tmux") {
		t.Fatalf("remote open command does not check tmux: %s", cmd)
	}
	if strings.Index(cmd, "command -v tmux") > strings.Index(cmd, "tmux new-session") {
		t.Fatalf("tmux check must happen before tmux new-session: %s", cmd)
	}
}

func TestOpenRemoteCommandRejectsInvalidTmuxNames(t *testing.T) {
	for _, tmuxName := range []string{
		"assh_abcdef12;touch_bad",
		"assh_../bad",
		"tmux_abcdef12",
		"assh_",
	} {
		t.Run(tmuxName, func(t *testing.T) {
			if _, err := OpenRemoteCommand(`{"sid":"abcdef12"}`, tmuxName); err == nil {
				t.Fatalf("OpenRemoteCommand() error = nil, want error")
			}
		})
	}
}

func TestExecRemoteCommandWritesSeqFiles(t *testing.T) {
	got, err := ExecRemoteCommand("abcdef12", "assh_abcdef12", 3, "echo a; echo b", 120)
	if err != nil {
		t.Fatalf("ExecRemoteCommand() error = %v", err)
	}

	for _, want := range []string{
		"~/.assh/sessions/abcdef12/3.out",
		"~/.assh/sessions/abcdef12/3.err",
		"~/.assh/sessions/abcdef12/3.rc",
		"{ echo a; echo b; } > ~/.assh/sessions/abcdef12/3.out 2> ~/.assh/sessions/abcdef12/3.err",
		"tmux send-keys",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ExecRemoteCommand() = %q, want to contain %q", got, want)
		}
	}
}

func TestExecRemoteCommandUsesConfiguredWaitSeconds(t *testing.T) {
	cmd, err := ExecRemoteCommand("abcdef12", "assh_abcdef12", 1, "true", 7)
	if err != nil {
		t.Fatalf("ExecRemoteCommand() error = %v", err)
	}
	if !strings.Contains(cmd, "while [ $i -lt 7 ]") {
		t.Fatalf("command does not use configured wait: %s", cmd)
	}
}

func TestExecRemoteCommandRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name     string
		sid      string
		tmuxName string
		seq      int
		command  string
	}{
		{name: "bad sid", sid: "../bad", tmuxName: "assh_../bad", seq: 1, command: "pwd"},
		{name: "bad tmux metachar", sid: "abcdef12", tmuxName: "assh_abcdef12;touch_bad", seq: 1, command: "pwd"},
		{name: "mismatched tmux", sid: "abcdef12", tmuxName: "assh_abcdef13", seq: 1, command: "pwd"},
		{name: "bad seq", sid: "abcdef12", tmuxName: "assh_abcdef12", seq: 0, command: "pwd"},
		{name: "empty command", sid: "abcdef12", tmuxName: "assh_abcdef12", seq: 1, command: "  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ExecRemoteCommand(tt.sid, tt.tmuxName, tt.seq, tt.command, 120); err == nil {
				t.Fatalf("ExecRemoteCommand() error = nil, want error")
			}
		})
	}
}

func TestExecRemoteCommandRejectsInvalidWaitSeconds(t *testing.T) {
	if _, err := ExecRemoteCommand("abcdef12", "assh_abcdef12", 1, "pwd", 0); err == nil {
		t.Fatalf("ExecRemoteCommand() error = nil, want error")
	}
}

func TestReadRemoteCommandBuildsPagedRead(t *testing.T) {
	got, err := ReadRemoteCommand("abcdef12", 3, "stderr", 4, 10)
	if err != nil {
		t.Fatalf("ReadRemoteCommand() error = %v", err)
	}

	for _, want := range []string{
		"~/.assh/sessions/abcdef12/3.err",
		"__ASSH_NOT_FOUND__",
		"tail -n +5",
		"head -n 10",
		"__ASSH_TOTAL_LINES__",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ReadRemoteCommand() = %q, want to contain %q", got, want)
		}
	}
}

func TestReadRemoteCommandRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name   string
		sid    string
		seq    int
		stream string
		offset int
		limit  int
	}{
		{name: "bad sid", sid: "../bad", seq: 1, stream: "stdout", offset: 0, limit: 1},
		{name: "bad seq", sid: "abcdef12", seq: 0, stream: "stdout", offset: 0, limit: 1},
		{name: "bad stream", sid: "abcdef12", seq: 1, stream: "bad", offset: 0, limit: 1},
		{name: "bad offset", sid: "abcdef12", seq: 1, stream: "stdout", offset: -1, limit: 1},
		{name: "offset overflow", sid: "abcdef12", seq: 1, stream: "stdout", offset: int(^uint(0) >> 1), limit: 1},
		{name: "bad limit", sid: "abcdef12", seq: 1, stream: "stdout", offset: 0, limit: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ReadRemoteCommand(tt.sid, tt.seq, tt.stream, tt.offset, tt.limit); err == nil {
				t.Fatalf("ReadRemoteCommand() error = nil, want error")
			}
		})
	}
}

func TestCloseRemoteCommandChecksMarker(t *testing.T) {
	got, err := CloseRemoteCommand("abcdef12", "assh_abcdef12")
	if err != nil {
		t.Fatalf("CloseRemoteCommand() error = %v", err)
	}

	for _, want := range []string{
		"command -v tmux",
		"created_by",
		"assh",
		"tmux has-session",
		"tmux kill-session",
		"rm -rf ~/.assh/sessions/abcdef12",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("CloseRemoteCommand() = %q, want to contain %q", got, want)
		}
	}
	if strings.Contains(got, "kill-session -t 'assh_abcdef12' 2>/dev/null || true") {
		t.Fatalf("CloseRemoteCommand() suppresses kill-session failure: %q", got)
	}
	if !strings.Contains(got, "tmux kill-session -t 'assh_abcdef12' || exit $?") {
		t.Fatalf("CloseRemoteCommand() does not propagate kill-session failure: %q", got)
	}
}

func TestGCRemoteCommandValidatesMetadataBeforeDelete(t *testing.T) {
	cmd, err := GCRemoteCommand("abcdef12", "assh_abcdef12")
	if err != nil {
		t.Fatalf("GCRemoteCommand() error = %v", err)
	}
	for _, want := range []string{
		"meta.json",
		"json.load",
		"created_by",
		"sid",
		"tmux_name",
		"tmux kill-session -t 'assh_abcdef12'",
		"rm -rf ~/.assh/sessions/abcdef12",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("GCRemoteCommand missing %q: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "grep -q") {
		t.Fatalf("GCRemoteCommand uses grep metadata validation: %s", cmd)
	}
}

func TestCloseRemoteCommandRejectsUnsafeInputs(t *testing.T) {
	for _, tt := range []struct {
		name     string
		sid      string
		tmuxName string
	}{
		{name: "bad sid", sid: "../bad", tmuxName: "assh_../bad"},
		{name: "missing prefix", sid: "abcdef12", tmuxName: "tmux_abcdef12"},
		{name: "mismatched", sid: "abcdef12", tmuxName: "assh_abcdef13"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CloseRemoteCommand(tt.sid, tt.tmuxName); err == nil {
				t.Fatalf("CloseRemoteCommand() error = nil, want error")
			}
		})
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
