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
	"strings"
	"time"
)

const (
	defaultRelativeSessionsDir = ".codex/sessions"
	maxLineSize                = 16 * 1024 * 1024 // 16 MiB, to safely fit large encrypted payloads
	snippetLimit               = 160
)

// Load discovers and parses Codex CLI sessions located under sessionsDir. When sessionsDir
// is empty, the default path of "~/.codex/sessions" is used.
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
			existing.LastAction = session.LastAction
			if session.WorkingDir != "" {
				existing.WorkingDir = session.WorkingDir
			}
		} else if existing.WorkingDir == "" && session.WorkingDir != "" {
			existing.WorkingDir = session.WorkingDir
		}

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

// ResolveDir returns the absolute directory where Codex session logs are stored. When dir is empty,
// the default "~/.codex/sessions" location is used.
func ResolveDir(dir string) (string, error) {
	if dir != "" {
		return filepath.Clean(dir), nil
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
		createdSet bool
		lastTS     time.Time
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
			var payload sessionMetaPayload
			if err := json.Unmarshal(entry.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode session_meta payload: %w", err)
			}
			session.ID = payload.ID
			session.WorkingDir = payload.CWD
			if pTs, pErr := parseTimestamp(payload.Timestamp); pErr == nil {
				session.CreatedAt = pTs
				createdSet = true
			}
		}

		if ts.After(lastTS) || lastTS.IsZero() {
			lastTS = ts
			if desc := describeEntry(entry); desc != "" {
				session.LastAction = desc
			} else if entry.Type == "session_meta" && session.LastAction == "" {
				session.LastAction = "session started"
			}
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

func describeEntry(entry logEntry) string {
	switch entry.Type {
	case "response_item":
		return describeResponseItem(entry.Payload)
	case "event_msg":
		return describeEventMessage(entry.Payload)
	default:
		return ""
	}
}

type responseItemPayload struct {
	Type      string             `json:"type"`
	Role      string             `json:"role,omitempty"`
	Content   []messageContent   `json:"content,omitempty"`
	Name      string             `json:"name,omitempty"`
	Arguments string             `json:"arguments,omitempty"`
	Output    string             `json:"output,omitempty"`
	Summary   []messageContent   `json:"summary,omitempty"`
	CallID    string             `json:"call_id,omitempty"`
	Metadata  json.RawMessage    `json:"metadata,omitempty"`
	Result    json.RawMessage    `json:"result,omitempty"`
	Error     *responseItemError `json:"error,omitempty"`
	Status    string             `json:"status,omitempty"`
	Title     string             `json:"title,omitempty"`
	AltText   string             `json:"alt_text,omitempty"`
	Media     []json.RawMessage  `json:"media,omitempty"`
	Parts     []json.RawMessage  `json:"parts,omitempty"`
	Kind      string             `json:"kind,omitempty"`
	Data      json.RawMessage    `json:"data,omitempty"`
	Encrypted json.RawMessage    `json:"encrypted_content,omitempty"`
}

type responseItemError struct {
	Message string `json:"message"`
}

type messageContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func describeResponseItem(raw json.RawMessage) string {
	var payload responseItemPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}

	switch payload.Type {
	case "message":
		text := firstNonEmptyText(payload.Content)
		if text == "" {
			text = firstNonEmptyText(payload.Summary)
		}
		if text == "" && payload.Title != "" {
			text = payload.Title
		}
		if text == "" {
			return ""
		}

		prefix := strings.TrimSpace(payload.Role)
		if prefix != "" {
			return fmt.Sprintf("%s: %s", prefix, compactSnippet(text))
		}
		return compactSnippet(text)
	case "reasoning":
		text := firstNonEmptyText(payload.Summary)
		if text == "" {
			text = firstNonEmptyText(payload.Content)
		}
		if text == "" {
			return ""
		}
		return fmt.Sprintf("reasoning: %s", compactSnippet(text))
	case "function_call":
		desc := fmt.Sprintf("call %s", payload.Name)
		if args := describeFunctionArguments(payload.Name, payload.Arguments); args != "" {
			desc = fmt.Sprintf("%s %s", desc, args)
		}
		return desc
	case "function_call_output":
		return describeFunctionOutput(payload)
	default:
		if payload.Title != "" {
			return compactSnippet(payload.Title)
		}
		return ""
	}
}

func describeFunctionArguments(name, argsJSON string) string {
	if argsJSON == "" {
		return ""
	}
	switch name {
	case "shell":
		var call struct {
			Command []string `json:"command"`
			Workdir string   `json:"workdir"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &call); err != nil {
			return ""
		}
		if len(call.Command) == 0 {
			return ""
		}
		cmd := strings.Join(call.Command, " ")
		return compactSnippet(cmd)
	default:
		return ""
	}
}

func describeFunctionOutput(payload responseItemPayload) string {
	if payload.Output == "" {
		if payload.Error != nil && payload.Error.Message != "" {
			return fmt.Sprintf("call %s error: %s", payload.Name, compactSnippet(payload.Error.Message))
		}
		return fmt.Sprintf("call %s completed", payload.Name)
	}

	var out struct {
		Output   string `json:"output"`
		Metadata struct {
			ExitCode *int `json:"exit_code"`
		} `json:"metadata"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload.Output), &out); err != nil {
		return fmt.Sprintf("call %s output", payload.Name)
	}

	switch {
	case out.Error != "":
		return fmt.Sprintf("call %s error: %s", payload.Name, compactSnippet(out.Error))
	case out.Metadata.ExitCode != nil:
		snippet := compactSnippet(out.Output)
		if snippet != "" {
			return fmt.Sprintf("call %s exit %d: %s", payload.Name, *out.Metadata.ExitCode, snippet)
		}
		return fmt.Sprintf("call %s exit %d", payload.Name, *out.Metadata.ExitCode)
	default:
		if out.Output == "" {
			return fmt.Sprintf("call %s completed", payload.Name)
		}
		return fmt.Sprintf("call %s: %s", payload.Name, compactSnippet(out.Output))
	}
}

type eventMsgPayload struct {
	Type    string          `json:"type"`
	Message string          `json:"message,omitempty"`
	Text    string          `json:"text,omitempty"`
	Kind    string          `json:"kind,omitempty"`
	Status  string          `json:"status,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Detail  json.RawMessage `json:"detail,omitempty"`
}

func describeEventMessage(raw json.RawMessage) string {
	var payload eventMsgPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	switch payload.Type {
	case "user_message", "assistant_message", "system_message":
		text := payload.Message
		if text == "" {
			text = payload.Text
		}
		if text == "" {
			return ""
		}
		return fmt.Sprintf("%s: %s", payload.Type, compactSnippet(text))
	case "tool_progress":
		if payload.Message != "" {
			return fmt.Sprintf("tool progress: %s", compactSnippet(payload.Message))
		}
	case "token_count":
		return "token usage updated"
	case "command_output":
		if payload.Message != "" {
			return fmt.Sprintf("command output: %s", compactSnippet(payload.Message))
		}
	}
	if payload.Message != "" {
		return fmt.Sprintf("%s: %s", payload.Type, compactSnippet(payload.Message))
	}
	return payload.Type
}

func firstNonEmptyText(items []messageContent) string {
	for _, item := range items {
		if strings.TrimSpace(item.Text) != "" {
			return item.Text
		}
	}
	return ""
}

func compactSnippet(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// Collapse whitespace similar to fzf preview.
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= snippetLimit {
		return text
	}
	if snippetLimit <= 3 {
		return text[:snippetLimit]
	}
	return text[:snippetLimit-3] + "..."
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
