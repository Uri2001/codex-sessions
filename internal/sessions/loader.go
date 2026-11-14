package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	defaultRelativeSessionsDir = ".codex/sessions"
	maxLineSize                = 16 * 1024 * 1024 // 16 MiB, to safely fit large encrypted payloads
)

// Load discovers and parses Codex CLI sessions located under sessionsDir. When sessionsDir
// is empty, ResolveDir selects the directory from CODEX_HOME or the user home.
func Load(sessionsDir string) ([]Session, error) {
	root, err := ResolveDir(sessionsDir)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Session{}, nil
		}
		return nil, fmt.Errorf("stat sessions dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sessions path %q is not a directory", root)
	}

	byID := make(map[string]*Session)
	var combinedErr error

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			combinedErr = errors.Join(combinedErr, fmt.Errorf("walk %s: %w", path, walkErr))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".jsonl" {
			return nil
		}

		session, err := parseSessionFile(path)
		if err != nil {
			combinedErr = errors.Join(combinedErr, fmt.Errorf("parse %s: %w", path, err))
			return nil
		}

		existing := byID[session.ID]
		if existing == nil {
			copySession := session.Snapshot()
			byID[session.ID] = &copySession
			return nil
		}

		// Merge data favouring the latest metadata.
		if session.CreatedAt.Before(existing.CreatedAt) || existing.CreatedAt.IsZero() {
			existing.CreatedAt = session.CreatedAt
		}
		if session.UpdatedAt.After(existing.UpdatedAt) {
			existing.UpdatedAt = session.UpdatedAt
			if session.WorkingDir != "" {
				existing.WorkingDir = session.WorkingDir
			}
		} else if existing.WorkingDir == "" && session.WorkingDir != "" {
			existing.WorkingDir = session.WorkingDir
		}

		existing.mergeRequests(session)

		for _, fp := range session.FilePaths {
			if !contains(existing.FilePaths, fp) {
				existing.FilePaths = append(existing.FilePaths, fp)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	combinedErr = errors.Join(combinedErr, loadSessionNames(root, byID))

	sessions := make([]Session, 0, len(byID))
	for _, s := range byID {
		// Ensure FilePaths sorted for determinism.
		sort.Strings(s.FilePaths)
		sessions = append(sessions, *s)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, combinedErr
}

// ResolveDir returns the directory where Codex session logs are stored.
// Priority: explicit dir, non-empty CODEX_HOME/sessions, then ~/.codex/sessions.
func ResolveDir(dir string) (string, error) {
	if dir != "" {
		return filepath.Clean(dir), nil
	}

	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		return filepath.Join(codexHome, "sessions"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("detect user home: %w", err)
	}
	return filepath.Join(home, filepath.FromSlash(defaultRelativeSessionsDir)), nil
}

func parseSessionFile(path string) (*Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, maxLineSize)
	session := &Session{
		FilePaths: []string{path},
	}

	var (
		metadataSet bool
		createdSet  bool
		lastTS      time.Time
	)

	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return nil, fmt.Errorf("line exceeds %d bytes", maxLineSize)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}

		line = bytesTrimRightNewline(line)
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}

		var entry logEntry
		if unmarshalErr := json.Unmarshal(line, &entry); unmarshalErr != nil {
			return nil, fmt.Errorf("decode log entry: %w", unmarshalErr)
		}

		ts, tsErr := parseTimestamp(entry.Timestamp)
		if tsErr != nil {
			ts = time.Time{}
		}

		switch entry.Type {
		case "session_meta":
			// Fork rollouts include inherited session_meta records after their own.
			// Only the first record identifies this file; keep reading later events.
			if metadataSet {
				break
			}
			var payload sessionMetaPayload
			if err := json.Unmarshal(entry.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode session_meta payload: %w", err)
			}
			metadataSet = true
			session.ID = payload.ID
			session.WorkingDir = payload.CWD
			if pTs, pErr := parseTimestamp(payload.Timestamp); pErr == nil {
				session.CreatedAt = pTs
				createdSet = true
			}
		}

		session.readRequest(entry, ts)
		if ts.After(lastTS) || lastTS.IsZero() {
			lastTS = ts
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	if session.ID == "" {
		return nil, errors.New("missing session id")
	}

	session.UpdatedAt = lastTS
	if !createdSet || session.CreatedAt.IsZero() {
		session.CreatedAt = session.UpdatedAt
	}

	return session, nil
}

func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("timestamp empty")
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid timestamp: %q", value)
}

type logEntry struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
}

func bytesTrimRightNewline(b []byte) []byte {
	return bytes.TrimRight(b, "\r\n")
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
