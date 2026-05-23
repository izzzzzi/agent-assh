package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var safeForwardName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type ForwardRecord struct {
	Name             string   `json:"name"`
	Host             string   `json:"host"`
	User             string   `json:"user"`
	Port             int      `json:"port"`
	Identity         string   `json:"identity,omitempty"`
	Jump             string   `json:"jump,omitempty"`
	HostKeyPolicy    string   `json:"host_key_policy"`
	Local            []string `json:"local,omitempty"`
	Remote           []string `json:"remote,omitempty"`
	Dynamic          []string `json:"dynamic,omitempty"`
	ControlSocket    string   `json:"control_socket"`
	CreatedAtRFC3339 string   `json:"created_at"`
	PersistSeconds   int64    `json:"persist_seconds"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
}

type ForwardStore struct {
	dir string
}

func NewForwardStore(baseDir string) *ForwardStore {
	return &ForwardStore{dir: filepath.Join(baseDir, "forward")}
}

func (s *ForwardStore) Save(record ForwardRecord) error {
	if !SafeForwardName(record.Name) {
		return errors.New("invalid forward name")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(s.path(record.Name), body)
}

func (s *ForwardStore) Load(name string) (ForwardRecord, error) {
	if !SafeForwardName(name) {
		return ForwardRecord{}, errors.New("invalid forward name")
	}
	body, err := os.ReadFile(s.path(name))
	if err != nil {
		return ForwardRecord{}, err
	}
	var record ForwardRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return ForwardRecord{}, err
	}
	return record, nil
}

func (s *ForwardStore) List() ([]ForwardRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]ForwardRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-len(".json")]
		record, err := s.Load(name)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Name < records[j].Name
	})
	return records, nil
}

func (s *ForwardStore) Delete(name string) error {
	if !SafeForwardName(name) {
		return errors.New("invalid forward name")
	}
	err := os.Remove(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func SafeForwardName(name string) bool {
	return safeForwardName.MatchString(name)
}

func (s *ForwardStore) path(name string) string {
	return filepath.Join(s.dir, name+".json")
}
