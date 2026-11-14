package sessions

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func requestRecord(ts, kind string, payload any) string {
	b, _ := json.Marshal(map[string]any{"timestamp": ts, "type": kind, "payload": payload})
	return string(b)
}

func userRecord(ts, text string) string {
	return requestRecord(ts, "event_msg", map[string]any{"type": "user_message", "message": text})
}

func responseRecord(ts, role string, texts ...string) string {
	var content []map[string]string
	for _, text := range texts {
		content = append(content, map[string]string{"type": "input_text", "text": text})
	}
	return requestRecord(ts, "response_item", map[string]any{"type": "message", "role": role, "content": content})
}

func TestLastRequestSelection(t *testing.T) {
	at := func(n int) string { return fmt.Sprintf("2026-04-04T00:00:%02dZ", n) }
	noise := []string{}
	for i, kind := range []string{"token_count", "task_complete", "task_completed", "turn_aborted", "tool_progress", "unknown_event"} {
		noise = append(noise, requestRecord(at(20+i), "event_msg", map[string]any{"type": kind, "message": "task complete"}))
	}
	for _, tc := range []struct {
		name    string
		records []string
		want    string
	}{
		{"latest user over later reply", []string{userRecord(at(1), "old"), userRecord(at(2), "Добавь авторизацию"), responseRecord(at(3), "assistant", "Готово")}, "Добавь авторизацию"},
		{"response messages and multiple parts", []string{responseRecord(at(1), "user", "<environment_context>cwd</environment_context>", "Почини\nпоиск", "и сортировку")}, "Почини поиск и сортировку"},
		{"assistant fallback", []string{responseRecord(at(1), "assistant", "Старый ответ"), requestRecord(at(2), "event_msg", map[string]any{"type": "assistant_message", "text": "Новый ответ"})}, "Новый ответ"},
		{"no conversation", nil, ""},
		{"system and tool messages", []string{responseRecord(at(1), "system", "Instructions"), responseRecord(at(2), "tool", "output"), requestRecord(at(3), "response_item", map[string]any{"type": "reasoning", "content": []any{}})}, ""},
		{"injected context after request", []string{userRecord(at(1), "Настрой сервер"), responseRecord(at(2), "user", "# AGENTS.md instructions for /project\nRules"), userRecord(at(3), "<environment_context>cwd</environment_context>"), userRecord(at(4), "  ")}, "Настрой сервер"},
		{"out of order messages", []string{userRecord(at(5), "newer"), userRecord(at(1), "older")}, "newer"},
		{"equal timestamps", []string{userRecord(at(1), "first"), userRecord(at(1), "second")}, "second"},
		{"legacy text field", []string{requestRecord(at(1), "event_msg", map[string]any{"type": "user_message", "text": "Запрос"})}, "Запрос"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := append([]string{parentMeta}, tc.records...)
			records = append(records, noise...)
			path := writeRollout(t, t.TempDir(), "test.jsonl", records...)
			s, err := parseSessionFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if s.LastRequest != tc.want {
				t.Fatalf("LastRequest = %q, want %q", s.LastRequest, tc.want)
			}
			wantUpdated, _ := time.Parse(time.RFC3339, at(25))
			if !s.UpdatedAt.Equal(wantUpdated) {
				t.Fatalf("UpdatedAt = %s, want %s", s.UpdatedAt, wantUpdated)
			}
		})
	}
}

func TestCleanMessage(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"<environment_context>cwd</environment_context>\nНастрой сервер", "Настрой сервер"},
		{"<user_instructions>rules</user_instructions>Запрос<instructions>rules</instructions>", "Запрос"},
		{"<environment_context>one</environment_context><environment_context>two</environment_context>Запрос", "Запрос"},
		{"<environment_context>unfinished", ""},
		{"# Context from my IDE setup:\n## Open tabs:\nsecret.go\n## My request for Codex:\nПочини поиск", "Почини поиск"},
		{"# AGENTS.md instructions for /project\nDo things", ""},
		{"Another language model started to solve this problem. Summary", ""},
		{"<turn_aborted>Cancelled</turn_aborted>", ""},
		{"  [red]Русский 日本語 😀\n\t текст ", "[red]Русский 日本語 😀 текст"},
	} {
		if got := cleanMessage(tc.in); got != tc.want {
			t.Errorf("cleanMessage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMergeLastRequests(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			root := t.TempDir()
			a, b := "a.jsonl", "b.jsonl"
			if reverse {
				a, b = b, a
			}
			writeRollout(t, root, a, parentMeta, userRecord("2026-04-04T00:00:01Z", "latest request"))
			writeRollout(t, root, b, parentMeta, userRecord("2026-04-03T00:00:00Z", "old request"), responseRecord("2026-04-05T00:00:00Z", "assistant", "reply"), requestRecord("2026-04-06T00:00:00Z", "event_msg", map[string]any{"type": "task_complete"}))
			items, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].LastRequest != "latest request" {
				t.Fatalf("merged sessions: %+v", items)
			}
			if !strings.HasPrefix(items[0].UpdatedAt.Format(time.RFC3339), "2026-04-06") {
				t.Fatalf("wrong activity time: %s", items[0].UpdatedAt)
			}
		})
	}
}
