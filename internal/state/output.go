package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/agent-ssh/assh/internal/ids"
)

type OutputPage struct {
	OutputID   string `json:"output_id"`
	Stream     string `json:"stream"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	TotalLines int    `json:"total_lines"`
	HasMore    bool   `json:"has_more"`
	Content    string `json:"content"`
}

type OutputStore struct {
	dir string
}

func NewOutputStore(dir string) *OutputStore {
	return &OutputStore{dir: dir}
}

func (s *OutputStore) Write(id string, stdout, stderr []byte) error {
	if !ids.Valid(id) {
		return errors.New("invalid output id")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	if err := writePrivateFile(s.path(id, "stdout"), stdout); err != nil {
		return err
	}
	return writePrivateFile(s.path(id, "stderr"), stderr)
}

func (s *OutputStore) Read(id, stream string, offset, limit int) (OutputPage, error) {
	if !ids.Valid(id) {
		return OutputPage{}, errors.New("invalid output id")
	}
	if stream != "stdout" && stream != "stderr" {
		return OutputPage{}, errors.New("invalid output stream")
	}
	if offset < 0 {
		return OutputPage{}, errors.New("offset must be non-negative")
	}
	if limit < 1 {
		return OutputPage{}, errors.New("limit must be at least 1")
	}

	data, err := os.ReadFile(s.path(id, stream))
	if err != nil {
		return OutputPage{}, err
	}
	lines := splitLines(string(data))
	total := len(lines)

	if offset >= total {
		return OutputPage{
			OutputID:   id,
			Stream:     stream,
			Offset:     offset,
			Limit:      limit,
			TotalLines: total,
			HasMore:    false,
			Content:    "",
		}, nil
	}

	remaining := total - offset
	if limit > remaining {
		limit = remaining
	}
	end := offset + limit

	content := strings.Join(lines[offset:end], "")

	return OutputPage{
		OutputID:   id,
		Stream:     stream,
		Offset:     offset,
		Limit:      limit,
		TotalLines: total,
		HasMore:    end < total,
		Content:    content,
	}, nil
}

func (s *OutputStore) path(id, stream string) string {
	if stream == "stderr" {
		return filepath.Join(s.dir, id+".err")
	}
	return filepath.Join(s.dir, id)
}

func writePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	// POSIX private modes are enforced on Unix. On Windows these modes are
	// best-effort; privacy relies on the per-user state directory.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil {
			return err
		}
		if err := os.Rename(tmpPath, path); err != nil {
			return err
		}
	}
	cleanup = false
	return os.Chmod(path, 0o600)
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	parts := strings.SplitAfter(content, "\n")
	if parts[len(parts)-1] == "" {
		return parts[:len(parts)-1]
	}
	return parts
}
