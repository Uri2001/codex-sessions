package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Uri2001/codex-sessions/internal/sessions"
	"github.com/rivo/tview"
)

func TestLastRequestColumnAndSearch(t *testing.T) {
	text := "[red]" + strings.Repeat("Я😀", 50) + " уникальныйхвост"
	m := newModel([]sessions.Session{{ID: "one", LastRequest: text}, {ID: "two"}}, "", "")
	m.table = tview.NewTable()
	m.applyFilter()
	m.refreshTable()
	if got := m.table.GetCell(0, 4).Text; got != "Last Request" {
		t.Fatalf("header = %q", got)
	}
	got := m.table.GetCell(1, 4).Text
	want := tview.Escape(string([]rune(text)[:77]) + "...")
	if got != want || !utf8.ValidString(got) {
		t.Fatalf("preview = %q, want %q", got, want)
	}
	if m.table.GetCell(2, 4).Text != "-" {
		t.Fatal("missing placeholder")
	}
	m.query = "уникальныйхвост"
	m.applyFilter()
	if len(m.filtered) != 1 || m.entries[m.filtered[0]].session.ID != "one" {
		t.Fatalf("full text search failed: %v", m.filtered)
	}
}
