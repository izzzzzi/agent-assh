package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

type RegistryEntry struct {
	SID           string    `json:"sid"`
	Label         string    `json:"label"`
	Host          string    `json:"host"`
	User          string    `json:"user"`
	Port          int       `json:"port"`
	Identity      string    `json:"identity,omitempty"`
	HostKeyPolicy string    `json:"host_key_policy"`
	TmuxName      string    `json:"tmux_name"`
	CreatedAt     time.Time `json:"created_at"`
	TTLSeconds    int64     `json:"ttl_seconds"`
	Seq           int       `json:"seq"`
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
		"command -v tmux >/dev/null 2>&1 || { echo tmux_missing >&2; exit 127; }",
		"mkdir -p " + sessionRoot,
		"mkdir -p " + sessionDir,
		"printf %s " + remote.SingleQuote(metaJSON) + " > " + metaPath,
		"tmux new-session -d -s " + remote.SingleQuote(tmuxName),
	}
	return strings.Join(parts, " && "), nil
}

func ExecRemoteCommand(sid, tmuxName string, seq int, command string, waitSeconds int) (string, error) {
	if err := validateSessionTarget(sid, tmuxName); err != nil {
		return "", err
	}
	if seq < 1 {
		return "", errors.New("seq must be positive")
	}
	if strings.TrimSpace(command) == "" {
		return "", errors.New("command required")
	}
	if waitSeconds < 1 {
		return "", errors.New("wait seconds must be positive")
	}

	dir := sessionDir(sid)
	seqText := strconv.Itoa(seq)
	waitText := strconv.Itoa(waitSeconds)
	out := dir + "/" + seqText + ".out"
	errPath := dir + "/" + seqText + ".err"
	rc := dir + "/" + seqText + ".rc"
	wrapped := "{ " + command + "; } > " + out + " 2> " + errPath + "; echo $? > " + rc
	return "mkdir -p " + dir + " && rm -f " + rc + " && " +
		"tmux send-keys -t " + remote.SingleQuote(tmuxName) + " " + remote.SingleQuote(wrapped) + " Enter; " +
		"i=0; while [ $i -lt " + waitText + " ] && [ ! -f " + rc + " ]; do i=$((i+1)); sleep 1; done; " +
		"test -f " + rc + " || { echo __ASSH_TIMEOUT__; exit 124; }; " +
		"printf '__ASSH_RC__=%s\\n' \"$(cat " + rc + ")\"; " +
		"printf '__ASSH_STDOUT_LINES__=%s\\n' \"$(wc -l < " + out + " 2>/dev/null || echo 0)\"; " +
		"printf '__ASSH_STDERR_LINES__=%s\\n' \"$(wc -l < " + errPath + " 2>/dev/null || echo 0)\"", nil
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
	quotedTmuxName := remote.SingleQuote(tmuxName)
	return "test -f " + metaPath + " || exit 0; " +
		"command -v tmux >/dev/null 2>&1 || { echo tmux_missing >&2; exit 127; }; " +
		"grep -q '\"created_by\":\"assh\"' " + metaPath + " || exit 3; " +
		"if tmux has-session -t " + quotedTmuxName + " 2>/dev/null; then tmux kill-session -t " + quotedTmuxName + " || exit $?; fi; " +
		"rm -rf " + dir, nil
}

func GCRemoteCommand(sid, tmuxName string) (string, error) {
	if err := validateSessionTarget(sid, tmuxName); err != nil {
		return "", err
	}

	dir := sessionDir(sid)
	metaPath := dir + "/meta.json"
	quotedTmuxName := remote.SingleQuote(tmuxName)
	return "test -f " + metaPath + " || exit 0; " +
		"command -v tmux >/dev/null 2>&1 || { echo tmux_missing >&2; exit 127; }; " +
		"grep -q '\"created_by\":\"assh\"' " + metaPath + " || exit 3; " +
		"grep -q '\"sid\":\"" + sid + "\"' " + metaPath + " || exit 3; " +
		"if tmux has-session -t " + quotedTmuxName + " 2>/dev/null; then tmux kill-session -t " + quotedTmuxName + " || exit $?; fi; " +
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

func SaveRegistry(baseDir string, entry RegistryEntry) error {
	if !remote.SafeSID(entry.SID) {
		return errors.New("invalid session id")
	}
	dir := filepath.Join(baseDir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, entry.SID+".json")
	tmp, err := os.CreateTemp(dir, entry.SID+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func LoadRegistry(baseDir, sid string) (RegistryEntry, error) {
	if !remote.SafeSID(sid) {
		return RegistryEntry{}, errors.New("invalid session id")
	}
	body, err := os.ReadFile(filepath.Join(baseDir, "sessions", sid+".json"))
	if err != nil {
		return RegistryEntry{}, err
	}
	var entry RegistryEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		return RegistryEntry{}, err
	}
	if entry.SID != sid || entry.TmuxName != "assh_"+sid {
		return RegistryEntry{}, errors.New("invalid registry entry")
	}
	return entry, nil
}

func DeleteRegistry(baseDir, sid string) error {
	if !remote.SafeSID(sid) {
		return errors.New("invalid session id")
	}
	err := os.Remove(filepath.Join(baseDir, "sessions", sid+".json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func ListRegistry(baseDir string) ([]RegistryEntry, error) {
	dir := filepath.Join(baseDir, "sessions")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sessions []RegistryEntry
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		sid := strings.TrimSuffix(entry.Name(), ".json")
		session, err := LoadRegistry(baseDir, sid)
		if err == nil {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (m Metadata) Expired(now time.Time) bool {
	if m.TTLSeconds <= 0 {
		return false
	}
	return m.CreatedAt.Add(time.Duration(m.TTLSeconds) * time.Second).Before(now)
}
