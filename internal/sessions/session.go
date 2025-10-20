package sessions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Session holds aggregated information for a single Codex CLI session.
type Session struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	WorkingDir string
	LastAction string
	FilePaths  []string
}

// Snapshot returns a shallow copy of the session. Useful when storing a copy for
// presentation logic without exposing the underlying slice for modification.
func (s Session) Snapshot() Session {
	paths := make([]string, len(s.FilePaths))
	copy(paths, s.FilePaths)
	s.FilePaths = paths
	return s
}

// DeleteFiles removes all files associated with the session. It makes a best-effort attempt to
// prune empty directories created for the session, walking upwards until the sessions root or an
// occupied directory is encountered.
func DeleteFiles(sess Session, sessionsRoot string) error {
	if sessionsRoot != "" {
		sessionsRoot = filepath.Clean(sessionsRoot)
	}

	var combined error
	for _, path := range sess.FilePaths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			combined = errors.Join(combined, fmt.Errorf("remove %s: %w", path, err))
			continue
		}
		cleanupParentDirectories(filepath.Dir(path), sessionsRoot)
	}
	return combined
}

func cleanupParentDirectories(start, stop string) {
	stop = filepath.Clean(stop)

	for dir := filepath.Clean(start); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if stop != "" {
			rel, err := filepath.Rel(stop, dir)
			if err != nil {
				break
			}
			if strings.HasPrefix(rel, "..") {
				break
			}
		}
		if err := os.Remove(dir); err != nil {
			break
		}
		if dir == stop {
			break
		}
	}
}
