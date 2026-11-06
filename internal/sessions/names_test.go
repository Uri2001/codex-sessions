package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestLoadSessionNames(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "sessions")
	writeTestSession(t, filepath.Join(root, "first"), "first")
	writeTestSession(t, filepath.Join(root, "second"), "second")
	writeTestSession(t, filepath.Join(root, "unnamed"), "unnamed")
	before, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	// Physical append order wins, even when wall-clock timestamps go backwards.
	index := `{"id":"first","thread_name":"Old name","updated_at":"2026-11-06T10:00:00Z"}
not json
{"id":"first","thread_name":"  Новый проект — 日本語  ","updated_at":"2026-11-05T10:00:00Z"}
{"id":"second","thread_name":"Other project","updated_at":"unknown"}
{"id":"first","thread_name":"   ","updated_at":"2026-11-07T10:00:00Z"}
{"id":"first","thread_name":123}
{"id":"unknown","thread_name":"Orphan"}
{"thread_name":"Missing ID"}
{"id":"first","thread_name":"unfinished`
	path := filepath.Join(home, "session_index.jsonl")
	if err := os.WriteFile(path, []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := map[string]string{"first": "Новый проект — 日本語", "second": "Other project", "unnamed": ""}
	if len(got) != len(before) {
		t.Fatalf("index changed session count: %d", len(got))
	}
	originals := make(map[string]Session)
	for _, item := range before {
		originals[item.ID] = item
	}
	for _, item := range got {
		if item.Name != wantNames[item.ID] {
			t.Errorf("%s name = %q, want %q", item.ID, item.Name, wantNames[item.ID])
		}
		item.Name = ""
		if !reflect.DeepEqual(item, originals[item.ID]) {
			t.Errorf("name index changed rollout metadata: %+v", item)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != index {
		t.Fatal("Load modified the name index")
	}
}

func TestLoadNamesOptional(t *testing.T) {
	for _, state := range []string{"missing", "empty", "malformed", "directory"} {
		t.Run(state, func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, "sessions")
			writeTestSession(t, root, "session")
			path := filepath.Join(home, "session_index.jsonl")
			switch state {
			case "empty", "malformed":
				var data []byte
				if state == "malformed" {
					data = []byte("{broken\n")
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Load(root)
			if (err != nil) != (state == "directory") {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 1 || got[0].ID != "session" || got[0].Name != "" {
				t.Fatalf("optional index prevented loading: %+v", got)
			}
		})
	}
}

func TestNamesFollowResolvedSessionsDirectory(t *testing.T) {
	for _, source := range []string{"codex-home", "default-unset", "default-empty", "explicit", "dot"} {
		t.Run(source, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", filepath.Join(home, "environment"))
			root := filepath.Join(home, "environment", "sessions")
			arg := ""
			if source == "explicit" || source == "dot" {
				root = filepath.Join(home, "custom", "sessions")
				arg = root
			}
			if source == "default-unset" || source == "default-empty" {
				homeEnv := "HOME"
				if runtime.GOOS == "windows" {
					homeEnv = "USERPROFILE"
				}
				t.Setenv(homeEnv, home)
				t.Setenv("CODEX_HOME", "")
				if source == "default-unset" {
					if err := os.Unsetenv("CODEX_HOME"); err != nil {
						t.Fatal(err)
					}
				}
				root = filepath.Join(home, ".codex", "sessions")
			}
			writeTestSession(t, root, "session")
			path := filepath.Join(filepath.Dir(root), "session_index.jsonl")
			// Complete JSON without a final newline is still a valid index entry.
			if err := os.WriteFile(path, []byte(`{"id":"session","thread_name":"Selected home","updated_at":"unknown"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if source == "dot" {
				t.Chdir(root)
				arg = "."
			}
			got, err := Load(arg)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Name != "Selected home" {
				t.Fatalf("wrong name index: %+v", got)
			}
		})
	}
}

func TestForkNamesUseOwnIDs(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "sessions")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeRollout(t, root, "parent.jsonl", parentMeta)
	writeRollout(t, root, "fork.jsonl", forkMeta, parentMeta)
	index := `{"id":"parent","thread_name":"Parent title","updated_at":"unknown"}
{"id":"fork","thread_name":"Fork title","updated_at":"unknown"}`
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("missing fork: %+v", got)
	}
	for _, s := range got {
		want := map[string]string{"parent": "Parent title", "fork": "Fork title"}[s.ID]
		if s.Name != want {
			t.Fatalf("%s name = %q, want %q", s.ID, s.Name, want)
		}
	}
}
