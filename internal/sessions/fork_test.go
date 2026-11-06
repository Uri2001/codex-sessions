package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const parentMeta = `{"timestamp":"2026-03-02T22:29:28.365Z","type":"session_meta","payload":{"id":"parent","timestamp":"2026-02-03T09:18:32.109Z","cwd":"/parent"}}`
const forkMeta = `{"timestamp":"2026-03-02T22:29:28.356Z","type":"session_meta","payload":{"id":"fork","forked_from_id":"parent","timestamp":"2026-03-02T22:29:28.111Z","cwd":"/fork"}}`
const forkAction = `{"timestamp":"2026-03-02T22:30:00Z","type":"event_msg","payload":{"type":"user_message","message":"continue the fork"}}`

func TestParseForkMetadata(t *testing.T) {
	for name, prefix := range map[string]string{"metadata-first": "", "event-before-metadata": `{"timestamp":"2026-03-02T22:29:00Z","type":"event_msg","payload":{"type":"token_count"}}`} {
		t.Run(name, func(t *testing.T) {
			path := writeRollout(t, t.TempDir(), "fork.jsonl", prefix, forkMeta, parentMeta, forkAction)
			sess, err := parseSessionFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if sess.ID != "fork" || sess.WorkingDir != "/fork" {
				t.Fatalf("fork identity overwritten: ID=%q cwd=%q", sess.ID, sess.WorkingDir)
			}
			wantCreated, _ := time.Parse(time.RFC3339Nano, "2026-03-02T22:29:28.111Z")
			wantUpdated, _ := time.Parse(time.RFC3339, "2026-03-02T22:30:00Z")
			if !sess.CreatedAt.Equal(wantCreated) || !sess.UpdatedAt.Equal(wantUpdated) {
				t.Fatalf("unexpected timestamps: created=%s updated=%s", sess.CreatedAt, sess.UpdatedAt)
			}
			if sess.LastAction != "user_message: continue the fork" {
				t.Fatalf("did not read events after inherited metadata: %q", sess.LastAction)
			}
		})
	}
}

func TestLoadForksRemainSeparate(t *testing.T) {
	root := t.TempDir()
	parentPath := writeRollout(t, root, "parent.jsonl", parentMeta)
	forkPath := writeRollout(t, root, "fork.jsonl", forkMeta, parentMeta, forkAction)
	nestedMeta := `{"timestamp":"2026-03-03T00:00:00Z","type":"session_meta","payload":{"id":"nested","forked_from_id":"fork","timestamp":"2026-03-03T00:00:00Z","cwd":"/nested"}}`
	nestedPath := writeRollout(t, root, "nested.jsonl", nestedMeta, forkMeta, parentMeta)
	items, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d sessions, want parent, fork and nested fork: %+v", len(items), items)
	}
	paths := map[string]string{"parent": parentPath, "fork": forkPath, "nested": nestedPath}
	for _, item := range items {
		want, ok := paths[item.ID]
		if !ok || len(item.FilePaths) != 1 || item.FilePaths[0] != want {
			t.Fatalf("session files incorrectly associated: %+v", item)
		}
		delete(paths, item.ID)
	}
	if len(paths) != 0 {
		t.Fatalf("missing sessions: %v", paths)
	}
}

func TestDeleteForkIsolation(t *testing.T) {
	for _, deleteID := range []string{"parent", "fork"} {
		t.Run(deleteID, func(t *testing.T) {
			root := t.TempDir()
			paths := map[string]string{
				"parent": writeRollout(t, root, "parent.jsonl", parentMeta),
				"fork":   writeRollout(t, root, "fork.jsonl", forkMeta, parentMeta, forkAction),
			}
			items, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			var selected *Session
			for i := range items {
				if items[i].ID == deleteID {
					selected = &items[i]
					break
				}
			}
			if selected == nil {
				t.Fatalf("session %q not found", deleteID)
			}
			keepID := "parent"
			if deleteID == "parent" {
				keepID = "fork"
			}
			before, err := os.ReadFile(paths[keepID])
			if err != nil {
				t.Fatal(err)
			}
			if err := DeleteFiles(*selected, root); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(paths[deleteID]); !os.IsNotExist(err) {
				t.Fatalf("selected file was not removed: %v", err)
			}
			after, err := os.ReadFile(paths[keepID])
			if err != nil {
				t.Fatalf("deleting %s also removed %s: %v", deleteID, keepID, err)
			}
			if string(after) != string(before) {
				t.Fatalf("deleting %s modified %s", deleteID, keepID)
			}
		})
	}
}

func TestFirstMetadataWithoutIDRejectsInheritedIdentity(t *testing.T) {
	first := `{"timestamp":"2026-03-02T22:29:28Z","type":"session_meta","payload":{"cwd":"/fork"}}`
	path := writeRollout(t, t.TempDir(), "broken-fork.jsonl", first, parentMeta)
	if _, err := parseSessionFile(path); err == nil || err.Error() != "missing session id" {
		t.Fatalf("expected missing session id, got %v", err)
	}
}

func writeRollout(t *testing.T, root, name string, entries ...string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(strings.Join(entries, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
