package sessions

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadSessionNames reads Codex's append-only name index beside the sessions root.
// Like Codex's batch name lookup, the last valid non-empty name for each ID wins.
func loadSessionNames(root string, byID map[string]*Session) error {
	if len(byID) == 0 {
		return nil
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve session name index: %w", err)
	}
	path := filepath.Join(filepath.Dir(absoluteRoot), "session_index.jsonl")
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open session name index: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxLineSize)
	for scanner.Scan() {
		var entry struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		// Ignore damaged lines (including a partially appended final record).
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		if sess := byID[entry.ID]; sess != nil && name != "" {
			sess.Name = name
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read session name index %s: %w", path, err)
	}
	return nil
}
