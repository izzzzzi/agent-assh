package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
)

type SessionOutputPage struct {
	SID        string    `json:"sid"`
	Seq        int       `json:"seq"`
	Stream     string    `json:"stream"`
	Offset     int       `json:"offset"`
	Limit      int       `json:"limit"`
	TotalLines int       `json:"total_lines"`
	Content    string    `json:"content"`
	CachedAt   time.Time `json:"cached_at"`
}

type SessionOutputStore struct {
	dir string
}

func NewSessionOutputStore(baseDir string) *SessionOutputStore {
	return &SessionOutputStore{dir: filepath.Join(baseDir, "session_outputs")}
}

func (s *SessionOutputStore) Write(page SessionOutputPage) error {
	if err := validateSessionOutputPage(page); err != nil {
		return err
	}
	if page.CachedAt.IsZero() {
		page.CachedAt = time.Now().UTC()
	}
	dir := filepath.Join(s.dir, page.SID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(page, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(filepath.Join(dir, sessionOutputFileName(page.Seq, page.Stream)), body)
}

func (s *SessionOutputStore) List(sid string) ([]SessionOutputPage, error) {
	if !remote.SafeSID(sid) {
		return nil, errors.New("invalid session id")
	}
	dir := filepath.Join(s.dir, sid)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pages := make([]SessionOutputPage, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var page SessionOutputPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		if page.SID != sid {
			continue
		}
		if err := validateSessionOutputPage(page); err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Seq != pages[j].Seq {
			return pages[i].Seq < pages[j].Seq
		}
		return pages[i].Stream < pages[j].Stream
	})
	return pages, nil
}

func validateSessionOutputPage(page SessionOutputPage) error {
	if !remote.SafeSID(page.SID) {
		return errors.New("invalid session id")
	}
	if page.Seq < 1 {
		return errors.New("seq must be positive")
	}
	if page.Stream != "stdout" && page.Stream != "stderr" {
		return errors.New("invalid stream")
	}
	if page.Offset < 0 {
		return errors.New("offset must be non-negative")
	}
	if page.Limit < 1 {
		return errors.New("limit must be at least 1")
	}
	if page.TotalLines < 0 {
		return errors.New("total lines must be non-negative")
	}
	return nil
}

func sessionOutputFileName(seq int, stream string) string {
	return "seq-" + strconv.Itoa(seq) + "-" + stream + ".json"
}
