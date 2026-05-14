package session

import (
	"strings"
	"time"

	"github.com/agent-ssh/assh/internal/remote"
)

type Metadata struct {
	CreatedBy  string    `json:"created_by"`
	SID        string    `json:"sid"`
	Label      string    `json:"label"`
	TmuxName   string    `json:"tmux_name"`
	CreatedAt  time.Time `json:"created_at"`
	TTLSeconds int64     `json:"ttl_seconds"`
	ClientID   string    `json:"client_id,omitempty"`
}

func NewMetadata(sid, label string, ttl time.Duration, clientID string) Metadata {
	return Metadata{
		CreatedBy:  "assh",
		SID:        sid,
		Label:      label,
		TmuxName:   "assh_" + sid,
		CreatedAt:  time.Now().UTC(),
		TTLSeconds: int64(ttl.Seconds()),
		ClientID:   clientID,
	}
}

func CanCleanup(m Metadata) bool {
	return m.CreatedBy == "assh" && m.SID != "" && m.TmuxName == "assh_"+m.SID
}

func OpenRemoteCommand(metaJSON string, tmuxName string) string {
	sid := strings.TrimPrefix(tmuxName, "assh_")
	sessionDir := "~/.assh/sessions/" + sid

	parts := []string{
		"mkdir -p ~/.assh/sessions",
		"mkdir -p " + sessionDir,
		"printf %s " + remote.SingleQuote(metaJSON) + " > " + sessionDir + "/meta.json",
		"tmux new-session -d -s " + remote.SingleQuote(tmuxName),
	}
	return strings.Join(parts, " && ")
}

func (m Metadata) Expired(now time.Time) bool {
	if m.TTLSeconds <= 0 {
		return false
	}
	return m.CreatedAt.Add(time.Duration(m.TTLSeconds) * time.Second).Before(now)
}
