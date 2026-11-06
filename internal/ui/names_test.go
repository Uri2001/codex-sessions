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

func TestSearchSessionNames(t *testing.T) {
	m := newModel([]sessions.Session{{ID: "one", Name: "Kubernetes migration"}, {ID: "two", Name: "Настройка сервера"}, {ID: "three"}}, "", "")
	for query, want := range map[string]string{"KUBERNETES": "one", "настройка": "two", "three": "three"} {
		m.query = query
		m.applyFilter()
		if len(m.filtered) != 1 || m.entries[m.filtered[0]].session.ID != want {
			t.Errorf("query %q matched %v, want %s", query, m.filtered, want)
		}
	}
}

func TestNameColumn(t *testing.T) {
	m := newModel([]sessions.Session{{ID: "one", Name: "[red]Проект 日本語\nsecond line"}, {ID: "two"}}, "", "")
	m.table = tview.NewTable()
	m.applyFilter()
	m.refreshTable()
	if got := m.table.GetCell(0, 1).Text; got != "Name" {
		t.Fatalf("missing name column: %q", got)
	}
	if got, want := m.table.GetCell(1, 1).Text, tview.Escape("[red]Проект 日本語 second line"); got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got := m.table.GetCell(2, 1).Text; got != "-" {
		t.Fatalf("unnamed session = %q", got)
	}
	if got := m.table.GetCell(1, 2).Text; got != "one" {
		t.Fatalf("name replaced session ID: %q", got)
	}
}

func TestResumeBySessionName(t *testing.T) {
	if os.Getenv("CODEX_SESSIONS_NAME_TEST") == "1" {
		app := tview.NewApplication().SetScreen(tcell.NewSimulationScreen(""))
		m := newModel([]sessions.Session{{ID: "other-id", Name: "Other work"}, {ID: "selected-id", Name: "Unique project"}}, "", "")
		queued := false
		app.SetAfterDrawFunc(func(tcell.Screen) {
			if queued {
				return
			}
			queued = true
			for _, r := range "unique" {
				app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, r, 0))
			}
			app.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
		})
		if err := m.runApplication(app); err != nil {
			t.Fatal(err)
		}
		if m.resumeID != "selected-id" {
			t.Fatalf("resume ID = %q, want selected-id", m.resumeID)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResumeBySessionName$")
	cmd.Env = append(os.Environ(), "CODEX_SESSIONS_NAME_TEST=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("TUI did not finish name selection: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("TUI selection failed: %v\n%s", err, output)
	}
}
