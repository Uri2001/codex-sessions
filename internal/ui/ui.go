package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Uri2001/codex-sessions/internal/sessions"
	"github.com/gdamore/tcell/v2"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/rivo/tview"
)

const (
	searchPrompt   = "Search> "
	defaultPageLen = 10
)

type row struct {
	session   sessions.Session
	searchKey string
}

type model struct {
	entries      []row
	filtered     []int
	selected     int
	pageSize     int
	query        string
	status       string
	sessionsRoot string
	resumeID     string

	app        *tview.Application
	searchView *tview.TextView
	infoView   *tview.TextView
	table      *tview.Table
	helpView   *tview.TextView
	statusView *tview.TextView
}

// Run launches the TUI and returns the session ID selected for resume, if any.
func Run(items []sessions.Session, sessionsRoot, initialStatus string) (string, error) {
	m := newModel(items, sessionsRoot, initialStatus)
	if err := m.run(); err != nil {
		return "", err
	}
	return m.resumeID, nil
}

func newModel(items []sessions.Session, sessionsRoot, initialStatus string) *model {
	rows := make([]row, len(items))
	for i, sess := range items {
		key := strings.ToLower(strings.Join([]string{
			sess.ID,
			sess.WorkingDir,
			sess.LastAction,
			sess.CreatedAt.Format(time.RFC3339),
			sess.UpdatedAt.Format(time.RFC3339),
		}, " "))
		rows[i] = row{
			session:   sess,
			searchKey: key,
		}
	}
	return &model{
		entries:      rows,
		pageSize:     defaultPageLen,
		status:       initialStatus,
		sessionsRoot: sessionsRoot,
	}
}

func (m *model) run() error {
	m.app = tview.NewApplication()

	m.searchView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(false).
		SetWrap(false)

	m.infoView = tview.NewTextView().
		SetDynamicColors(false).
		SetRegions(false).
		SetWrap(false)

	m.table = tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0)

	m.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorBlue).Foreground(tcell.ColorWhite))
	m.table.SetSelectionChangedFunc(func(row, column int) {
		if row <= 0 || len(m.filtered) == 0 {
			m.selected = 0
			return
		}
		idx := row - 1
		if idx >= len(m.filtered) {
			idx = len(m.filtered) - 1
		}
		m.selected = idx
	})
	m.table.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		visible := height - 1 // header row
		if visible < 1 {
			visible = 1
		}
		m.pageSize = visible
		return x, y, width, height
	})

	m.helpView = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetText("[green]Up/Down move  PgUp/PgDn page  Enter resume  Del delete  Type to search  Backspace delete  Esc clear/exit  Ctrl+C quit")

	m.statusView = tview.NewTextView().
		SetDynamicColors(false).
		SetWrap(false)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(m.searchView, 1, 0, false).
		AddItem(listSpacer(), 1, 0, false).
		AddItem(m.infoView, 1, 0, false).
		AddItem(m.table, 0, 1, true).
		AddItem(listSpacer(), 1, 0, false).
		AddItem(m.helpView, 1, 0, false).
		AddItem(m.statusView, 1, 0, false)

	m.app.SetRoot(layout, true)
	m.app.SetFocus(m.table)

	m.app.SetInputCapture(m.handleEvent)

	m.applyFilter()
	m.refreshSearchView()
	m.refreshInfoView()
	m.refreshTable()
	m.setStatus(m.status)

	return m.app.Run()
}

func (m *model) handleEvent(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyRune:
		r := event.Rune()
		if unicode.IsControl(r) {
			return event
		}
		m.query += string(r)
		m.applyFilter()
		m.refreshSearchView()
		m.refreshInfoView()
		m.refreshTable()
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if m.query != "" {
			m.query = dropLastRune(m.query)
			m.applyFilter()
			m.refreshSearchView()
			m.refreshInfoView()
			m.refreshTable()
		}
		return nil
	case tcell.KeyEsc:
		if m.query != "" {
			m.query = ""
			m.applyFilter()
			m.refreshSearchView()
			m.refreshInfoView()
			m.refreshTable()
			return nil
		}
		m.resumeID = ""
		m.app.Stop()
		return nil
	case tcell.KeyEnter:
		if len(m.filtered) == 0 {
			return nil
		}
		idx := m.filtered[m.selected]
		m.resumeID = m.entries[idx].session.ID
		m.app.Stop()
		return nil
	case tcell.KeyDelete:
		m.deleteSelected()
		m.refreshSearchView()
		m.refreshInfoView()
		m.refreshTable()
		return nil
	case tcell.KeyPgDn:
		m.moveSelectionBy(m.pageSize)
		return nil
	case tcell.KeyPgUp:
		m.moveSelectionBy(-m.pageSize)
		return nil
	case tcell.KeyCtrlC:
		m.resumeID = ""
		m.app.Stop()
		return nil
	}
	return event
}

func (m *model) moveSelectionBy(delta int) {
	if len(m.filtered) == 0 {
		return
	}
	next := m.selected + delta
	if next < 0 {
		next = 0
	} else if next >= len(m.filtered) {
		next = len(m.filtered) - 1
	}
	m.selected = next
	m.table.Select(m.selected+1, 0)
}

func (m *model) refreshSearchView() {
	m.searchView.SetText(fmt.Sprintf("[blue::b]%s[-:-:-]%s", searchPrompt, m.query))
}

func (m *model) refreshInfoView() {
	total := len(m.entries)
	matches := len(m.filtered)
	displaying := matches
	if displaying > m.pageSize {
		displaying = m.pageSize
	}
	info := fmt.Sprintf("Matches: %d / Total: %d | Showing: %d", matches, total, displaying)
	m.infoView.SetText(info)
}

func (m *model) refreshTable() {
	m.table.Clear()

	headerStyle := tcell.StyleDefault.Bold(true)
	m.table.SetCell(0, 0, tview.NewTableCell("Updated").
		SetSelectable(false).
		SetStyle(headerStyle))
	m.table.SetCell(0, 1, tview.NewTableCell("Session ID").
		SetSelectable(false).
		SetStyle(headerStyle))
	m.table.SetCell(0, 2, tview.NewTableCell("Directory").
		SetSelectable(false).
		SetStyle(headerStyle))
	m.table.SetCell(0, 3, tview.NewTableCell("Last Action").
		SetSelectable(false).
		SetStyle(headerStyle))

	for i, idx := range m.filtered {
		sess := m.entries[idx].session
		row := i + 1
		m.table.SetCell(row, 0, tview.NewTableCell(formatTimestamp(sess.UpdatedAt)).
			SetExpansion(1))
		m.table.SetCell(row, 1, tview.NewTableCell(sess.ID).
			SetExpansion(1))
		m.table.SetCell(row, 2, tview.NewTableCell(abbreviatePath(sess.WorkingDir, 40)).
			SetExpansion(1))
		m.table.SetCell(row, 3, tview.NewTableCell(truncateText(sess.LastAction, 80)).
			SetExpansion(2))
	}

	if len(m.filtered) > 0 {
		if m.selected >= len(m.filtered) {
			m.selected = len(m.filtered) - 1
		}
		m.table.Select(m.selected+1, 0)
	} else {
		m.table.Select(0, 0)
	}
}

func (m *model) deleteSelected() {
	if len(m.filtered) == 0 {
		m.setStatus("Nothing to delete")
		return
	}
	idx := m.filtered[m.selected]
	sess := m.entries[idx].session
	if err := sessions.DeleteFiles(sess, m.sessionsRoot); err != nil {
		m.setStatus(fmt.Sprintf("Delete failed: %v", err))
		return
	}
	m.entries = append(m.entries[:idx], m.entries[idx+1:]...)
	m.setStatus(fmt.Sprintf("Session %s deleted", sess.ID))
	m.applyFilter()
}

func (m *model) setStatus(text string) {
	m.status = text
	m.statusView.SetText(text)
}

func (m *model) applyFilter() {
	if len(m.entries) == 0 {
		m.filtered = nil
		m.selected = 0
		return
	}

	query := strings.TrimSpace(m.query)
	if query == "" {
		m.filtered = make([]int, len(m.entries))
		for i := range m.entries {
			m.filtered[i] = i
		}
	} else {
		keys := make([]string, len(m.entries))
		for i, entry := range m.entries {
			keys[i] = entry.searchKey
		}
		results := fuzzy.RankFindFold(query, keys)
		sort.Slice(results, func(i, j int) bool {
			a, b := results[i], results[j]
			if a.Distance == b.Distance {
				sessA := m.entries[a.OriginalIndex].session
				sessB := m.entries[b.OriginalIndex].session
				if sessA.UpdatedAt.Equal(sessB.UpdatedAt) {
					return sessA.ID < sessB.ID
				}
				return sessA.UpdatedAt.After(sessB.UpdatedAt)
			}
			return a.Distance < b.Distance
		})
		m.filtered = m.filtered[:0]
		for _, rank := range results {
			m.filtered = append(m.filtered, rank.OriginalIndex)
		}
	}

	if len(m.filtered) == 0 {
		m.selected = 0
		return
	}

	if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func dropLastRune(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	if len(runes) <= 1 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func truncateText(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "-"
	}
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func abbreviatePath(path string, max int) string {
	if max <= 0 {
		return path
	}
	if len(path) <= max {
		return path
	}
	const ellipsis = "..."
	if max <= len(ellipsis) {
		return path[:max]
	}
	return ellipsis + path[len(path)-(max-1):]
}
