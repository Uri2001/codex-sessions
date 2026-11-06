package sessions

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSessionDirectory(t *testing.T) {
	for _, source := range []string{"explicit", "codex-home", "default-unset", "default-empty"} {
		t.Run(source, func(t *testing.T) {
			for _, state := range []string{"missing", "empty", "unrelated-files", "session"} {
				t.Run(state, func(t *testing.T) {
					home := t.TempDir()
					homeEnv := "HOME"
					if runtime.GOOS == "windows" {
						homeEnv = "USERPROFILE"
					}
					t.Setenv(homeEnv, home)
					t.Setenv("CODEX_HOME", filepath.Join(home, "custom-codex"))
					explicit := ""
					want := filepath.Join(home, ".codex", "sessions")
					switch source {
					case "explicit":
						explicit = filepath.Join(home, "explicit", "child", "..")
						want = filepath.Clean(explicit)
					case "codex-home":
						want = filepath.Join(os.Getenv("CODEX_HOME"), "sessions")
					case "default-unset":
						// Setenv registers restoration of the original value before Unsetenv.
						if err := os.Unsetenv("CODEX_HOME"); err != nil {
							t.Fatal(err)
						}
					case "default-empty":
						t.Setenv("CODEX_HOME", "")
					}
					// Reproduce ~/.codex existing without sessions, and ensure lower-priority
					// directories with logs are never used as a fallback.
					fallback := filepath.Join(home, ".codex", "sessions")
					if err := os.MkdirAll(filepath.Dir(fallback), 0o755); err != nil {
						t.Fatal(err)
					}
					if want != fallback {
						writeTestSession(t, fallback, "fallback")
					}
					if source == "explicit" {
						writeTestSession(t, filepath.Join(os.Getenv("CODEX_HOME"), "sessions"), "environment")
					}
					if state != "missing" {
						if err := os.MkdirAll(want, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					switch state {
					case "unrelated-files":
						if err := os.WriteFile(filepath.Join(want, "notes.txt"), []byte("not a session"), 0o600); err != nil {
							t.Fatal(err)
						}
					case "session":
						writeTestSession(t, filepath.Join(want, "2026", "04", "04"), "selected")
					}
					got, err := ResolveDir(explicit)
					if err != nil {
						t.Fatal(err)
					}
					if got != want {
						t.Fatalf("ResolveDir(%q) = %q, want %q", explicit, got, want)
					}
					items, err := Load(explicit)
					if err != nil {
						t.Fatal(err)
					}
					if state == "session" {
						if len(items) != 1 || items[0].ID != "selected" {
							t.Fatalf("unexpected sessions: %+v", items)
						}
					} else if len(items) != 0 {
						t.Fatalf("expected empty sessions, got %+v", items)
					}
					if state == "missing" {
						if _, err := os.Stat(want); !os.IsNotExist(err) {
							t.Fatalf("Load created missing directory or stat failed: %v", err)
						}
					}
				})
			}
		})
	}
}

func writeTestSession(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"timestamp":"2026-04-04T00:00:00Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"/tmp/project"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout.jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
