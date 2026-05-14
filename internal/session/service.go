package session

import (
	"errors"
	"strconv"
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
	return m.CreatedBy == "assh" && remote.SafeSID(m.SID) && m.TmuxName == "assh_"+m.SID
}

func OpenRemoteCommand(metaJSON string, tmuxName string) (string, error) {
	if !strings.HasPrefix(tmuxName, "assh_") {
		return "", errors.New("tmux name must start with assh_")
	}
	sid := strings.TrimPrefix(tmuxName, "assh_")
	if err := validateSessionTarget(sid, tmuxName); err != nil {
		return "", err
	}

	sessionRoot := "~/.assh/sessions"
	sessionDir := sessionRoot + "/" + sid
	metaPath := sessionDir + "/meta.json"

	parts := []string{
		"mkdir -p " + sessionRoot,
		"mkdir -p " + sessionDir,
		"printf %s " + remote.SingleQuote(metaJSON) + " > " + metaPath,
		"tmux new-session -d -s " + remote.SingleQuote(tmuxName),
	}
	return strings.Join(parts, " && "), nil
}

func ExecRemoteCommand(sid, tmuxName string, seq int, command string) (string, error) {
	if err := validateSessionTarget(sid, tmuxName); err != nil {
		return "", err
	}
	if seq < 1 {
		return "", errors.New("seq must be positive")
	}
	if strings.TrimSpace(command) == "" {
		return "", errors.New("command required")
	}

	dir := sessionDir(sid)
	seqText := strconv.Itoa(seq)
	out := dir + "/" + seqText + ".out"
	errPath := dir + "/" + seqText + ".err"
	rc := dir + "/" + seqText + ".rc"
	wrapped := command + " > " + out + " 2> " + errPath + "; echo $? > " + rc
	return "mkdir -p " + dir + " && tmux send-keys -t " + remote.SingleQuote(tmuxName) + " " + remote.SingleQuote(wrapped) + " Enter", nil
}

func ReadRemoteCommand(sid string, seq int, stream string, offset int, limit int) (string, error) {
	if !remote.SafeSID(sid) {
		return "", errors.New("invalid session id")
	}
	if seq < 1 {
		return "", errors.New("seq must be positive")
	}
	if stream != "stdout" && stream != "stderr" {
		return "", errors.New("invalid stream")
	}
	if offset < 0 || offset == int(^uint(0)>>1) || limit < 1 {
		return "", errors.New("invalid pagination")
	}

	ext := "out"
	if stream == "stderr" {
		ext = "err"
	}
	file := sessionDir(sid) + "/" + strconv.Itoa(seq) + "." + ext
	start := offset + 1
	return "test -f " + file + " || { echo __ASSH_NOT_FOUND__; exit 0; }; " +
		"total=$(wc -l < " + file + "); " +
		"tail -n +" + strconv.Itoa(start) + " " + file + " | head -n " + strconv.Itoa(limit) + "; " +
		"printf '\\n__ASSH_TOTAL_LINES__=%s\\n' \"$total\"", nil
}

func CloseRemoteCommand(sid, tmuxName string) (string, error) {
	if err := validateSessionTarget(sid, tmuxName); err != nil {
		return "", err
	}

	dir := sessionDir(sid)
	metaPath := dir + "/meta.json"
	return "test -f " + metaPath + " || exit 0; " +
		"grep -q '\"created_by\":\"assh\"' " + metaPath + " || exit 3; " +
		"tmux kill-session -t " + remote.SingleQuote(tmuxName) + " 2>/dev/null || true; " +
		"rm -rf " + dir, nil
}

func validateSessionTarget(sid, tmuxName string) error {
	if !remote.SafeSID(sid) {
		return errors.New("invalid session id")
	}
	if tmuxName != "assh_"+sid {
		return errors.New("tmux name does not match session id")
	}
	return nil
}

func sessionDir(sid string) string {
	return "~/.assh/sessions/" + sid
}

func (m Metadata) Expired(now time.Time) bool {
	if m.TTLSeconds <= 0 {
		return false
	}
	return m.CreatedAt.Add(time.Duration(m.TTLSeconds) * time.Second).Before(now)
}
