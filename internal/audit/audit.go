package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Event struct {
	Timestamp   time.Time `json:"ts"`
	Action      string    `json:"action"`
	Host        string    `json:"host,omitempty"`
	User        string    `json:"user,omitempty"`
	CommandHash string    `json:"command_hash,omitempty"`
	ExitCode    int       `json:"exit_code,omitempty"`
	StdoutLines int       `json:"stdout_lines,omitempty"`
	StderrLines int       `json:"stderr_lines,omitempty"`
}

func Write(path string, event Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}
