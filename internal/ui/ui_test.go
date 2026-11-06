package ui

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Uri2001/codex-sessions/internal/sessions"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Use a subprocess so a blocked event loop cannot hang the test runner.
func TestEmptyListCtrlC(t *testing.T) {
	if scenario := os.Getenv("CODEX_SESSIONS_UI_TEST"); scenario != "" {
		screen := tcell.NewSimulationScreen("")
		app := tview.NewApplication().SetScreen(screen)
		m := newModel(nil, t.TempDir(), "")
		var keys []*tcell.EventKey
		switch scenario {
		case "empty":
		case "empty-down":
			keys = append(keys, tcell.NewEventKey(tcell.KeyDown, 0, 0))
		case "empty-up":
			keys = append(keys, tcell.NewEventKey(tcell.KeyUp, 0, 0))
		case "no-matches", "no-matches-clear":
			m = newModel([]sessions.Session{{ID: "session-1"}}, t.TempDir(), "")
			keys = append(keys, tcell.NewEventKey(tcell.KeyRune, 'Ω', 0), tcell.NewEventKey(tcell.KeyDown, 0, 0))
			if scenario == "no-matches-clear" {
				keys = append(keys, tcell.NewEventKey(tcell.KeyEsc, 0, 0))
			}
		case "delete-last":
			m = newModel([]sessions.Session{{ID: "session-1"}}, t.TempDir(), "")
			keys = append(keys, tcell.NewEventKey(tcell.KeyDelete, 0, 0), tcell.NewEventKey(tcell.KeyDown, 0, 0))
		default:
			t.Fatalf("unknown scenario %q", scenario)
		}
		keys = append(keys, tcell.NewEventKey(tcell.KeyCtrlC, 0, 0))
		queued := false
		app.SetAfterDrawFunc(func(tcell.Screen) {
			if !queued {
				queued = true
				for _, key := range keys {
					app.QueueEvent(key)
				}
			}
		})
		if err := m.runApplication(app); err != nil {
			t.Fatal(err)
		}
		wantMatches := 0
		if scenario == "no-matches-clear" {
			wantMatches = 1
		}
		if len(m.filtered) != wantMatches {
			t.Fatalf("got %d matches, want %d", len(m.filtered), wantMatches)
		}
		if m.resumeID != "" {
			t.Fatalf("unexpected resume ID %q", m.resumeID)
		}
		return
	}
	for _, scenario := range []string{"empty", "empty-down", "empty-up", "no-matches", "no-matches-clear", "delete-last"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEmptyListCtrlC$")
			cmd.Env = append(os.Environ(), "CODEX_SESSIONS_UI_TEST="+scenario)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("TUI did not exit after Ctrl+C: %v\n%s", ctx.Err(), output)
			}
			if err != nil {
				t.Fatalf("TUI failed: %v\n%s", err, output)
			}
		})
	}
}
