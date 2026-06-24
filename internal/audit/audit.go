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
	SID         string    `json:"sid,omitempty"`
	Host        string    `json:"host,omitempty"`
	User        string    `json:"user,omitempty"`
	CommandHash string    `json:"command_hash,omitempty"`
	ExitCode    int       `json:"exit_code,omitempty"`
	StdoutLines int       `json:"stdout_lines,omitempty"`
	StderrLines int       `json:"stderr_lines,omitempty"`
	// RawLines and ServedLines track the metadata-first token economy: how many
	// lines existed versus how many were served to the agent on a read. Only read
	// actions populate them; `audit --savings` filters by action and treats a
	// missing field as 0, so omitempty here does not skew aggregation.
	RawLines    int `json:"raw_lines,omitempty"`
	ServedLines int `json:"served_lines,omitempty"`
	// Safety policy fields are populated only when a deny-only policy overlay
	// blocks a command, so operators can audit which policy fired.
	SafetyRule       string `json:"safety_rule,omitempty"`
	SafetyPolicyPath string `json:"safety_policy_path,omitempty"`
	SafetyPolicyHash string `json:"safety_policy_hash,omitempty"`
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
	SID    string
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
		if filter.SID != "" && event.SID != filter.SID {
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
