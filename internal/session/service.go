package session

import "time"

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

func (m Metadata) Expired(now time.Time) bool {
	if m.TTLSeconds <= 0 {
		return false
	}
	return m.CreatedAt.Add(time.Duration(m.TTLSeconds) * time.Second).Before(now)
}
