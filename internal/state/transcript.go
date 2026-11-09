package state

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/izzzzzi/agent-assh/internal/remote"
)

type TranscriptStore struct {
	dir string
}

func NewTranscriptStore(baseDir string) *TranscriptStore {
	return &TranscriptStore{dir: filepath.Join(baseDir, "transcripts")}
}

func (t *TranscriptStore) Append(sid string, seq int, command string, stdout []byte, stderr []byte) error {
	if !remote.SafeSID(sid) {
		return errors.New("invalid session id")
	}

	dir := filepath.Join(t.dir, sid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	path := filepath.Join(dir, "transcript.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	ts := time.Now().UTC().Format(time.RFC3339)
	_, err = f.WriteString("=== seq=" + strconv.Itoa(seq) + " ts=" + ts + " ===\n")
	if err != nil {
		return err
	}
	_, err = f.WriteString("$ " + command + "\n")
	if err != nil {
		return err
	}
	if len(stdout) > 0 {
		_, err = f.Write(stdout)
		if err != nil {
			return err
		}
		if stdout[len(stdout)-1] != '\n' {
			_, _ = f.WriteString("\n")
		}
	}
	if len(stderr) > 0 {
		_, err = f.WriteString("[stderr]\n")
		if err != nil {
			return err
		}
		_, err = f.Write(stderr)
		if err != nil {
			return err
		}
		if stderr[len(stderr)-1] != '\n' {
			_, _ = f.WriteString("\n")
		}
	}
	return nil
}
