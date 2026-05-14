package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func Write(path string, event Event) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()

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

type Filter struct {
	Last   int
	Host   string
	Failed bool
}

func Read(path string, filter Filter) ([]Event, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, err
		}
		if filter.Host != "" && event.Host != filter.Host {
			continue
		}
		if filter.Failed && event.ExitCode == 0 {
			continue
		}
		events = append(events, event)
	}
	if filter.Last > 0 && len(events) > filter.Last {
		events = events[len(events)-filter.Last:]
	}
	return events, nil
}
