package session

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/izzzzzi/agent-assh/internal/audit"
	"github.com/izzzzzi/agent-assh/internal/state"
)

type ExportResult struct {
	OK            bool     `json:"ok"`
	SID           string   `json:"sid"`
	Session       string   `json:"session"`
	Archive       string   `json:"archive"`
	Bytes         int64    `json:"bytes"`
	IncludedFiles []string `json:"included_files"`
}

type exportManifest struct {
	Tool          string    `json:"tool"`
	Format        string    `json:"format"`
	SID           string    `json:"sid"`
	Session       string    `json:"session"`
	Host          string    `json:"host"`
	User          string    `json:"user"`
	CreatedAt     time.Time `json:"created_at"`
	ExportedAt    time.Time `json:"exported_at"`
	IncludedFiles []string  `json:"included_files"`
}

func Export(baseDir, sid, archivePath string) (ExportResult, error) {
	entry, err := LoadRegistry(baseDir, sid)
	if err != nil {
		return ExportResult{}, err
	}
	if archivePath == "" {
		archivePath = filepath.Join(baseDir, "exports", sid+".tar.gz")
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		return ExportResult{}, err
	}

	events, err := exportAuditEvents(baseDir, entry)
	if err != nil {
		return ExportResult{}, err
	}
	outputs, err := state.NewSessionOutputStore(baseDir).List(sid)
	if err != nil {
		return ExportResult{}, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(archivePath), filepath.Base(archivePath)+".tmp-*")
	if err != nil {
		return ExportResult{}, err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	included, err := writeExportArchive(tmp, entry, events, outputs)
	closeErr := tmp.Close()
	if err != nil {
		return ExportResult{}, err
	}
	if closeErr != nil {
		return ExportResult{}, closeErr
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return ExportResult{}, err
	}
	if err := os.Rename(tmpPath, archivePath); err != nil {
		if removeErr := os.Remove(archivePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return ExportResult{}, err
		}
		if err := os.Rename(tmpPath, archivePath); err != nil {
			return ExportResult{}, err
		}
	}
	cleanup = false
	if err := os.Chmod(archivePath, 0o600); err != nil {
		return ExportResult{}, err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return ExportResult{}, err
	}
	return ExportResult{
		OK:            true,
		SID:           sid,
		Session:       entry.Label,
		Archive:       archivePath,
		Bytes:         info.Size(),
		IncludedFiles: included,
	}, nil
}

func writeExportArchive(file *os.File, entry RegistryEntry, events []audit.Event, outputs []state.SessionOutputPage) (included []string, err error) {
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	defer func() {
		if closeErr := tw.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if closeErr := gz.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	included = []string{"manifest.json", "session.json", "audit.jsonl"}
	for _, page := range outputs {
		included = append(included, outputArchiveName(page))
	}
	sort.Strings(included)

	manifest := exportManifest{
		Tool:          "assh",
		Format:        "assh-session-export-v1",
		SID:           entry.SID,
		Session:       entry.Label,
		Host:          entry.Host,
		User:          entry.User,
		CreatedAt:     entry.CreatedAt,
		ExportedAt:    time.Now().UTC(),
		IncludedFiles: included,
	}
	if err := writeJSONTarFile(tw, "manifest.json", manifest); err != nil {
		return nil, err
	}
	if err := writeJSONTarFile(tw, "session.json", entry); err != nil {
		return nil, err
	}
	if err := writeTarFile(tw, "audit.jsonl", []byte(auditJSONLines(events))); err != nil {
		return nil, err
	}
	for _, page := range outputs {
		if err := writeJSONTarFile(tw, outputArchiveName(page), page); err != nil {
			return nil, err
		}
	}
	return included, nil
}

func exportAuditEvents(baseDir string, entry RegistryEntry) ([]audit.Event, error) {
	events, err := audit.Read(filepath.Join(baseDir, "audit", "audit.jsonl"), audit.Filter{Host: entry.Host})
	if err != nil {
		return nil, err
	}
	filtered := make([]audit.Event, 0, len(events))
	for _, event := range events {
		if event.User != "" && event.User != entry.User {
			continue
		}
		if !strings.HasPrefix(event.Action, "session_") {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered, nil
}

func auditJSONLines(events []audit.Event) string {
	var b strings.Builder
	for _, event := range events {
		body, err := json.Marshal(event)
		if err != nil {
			continue
		}
		b.Write(body)
		b.WriteByte('\n')
	}
	return b.String()
}

func writeJSONTarFile(tw *tar.Writer, name string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeTarFile(tw, name, body)
}

func writeTarFile(tw *tar.Writer, name string, body []byte) error {
	header := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(body)),
		ModTime: time.Unix(0, 0).UTC(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

func outputArchiveName(page state.SessionOutputPage) string {
	return "outputs/seq-" + strconv.Itoa(page.Seq) + "-" + page.Stream + ".json"
}
