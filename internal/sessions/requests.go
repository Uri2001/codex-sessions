package sessions

import (
	"encoding/json"
	"strings"
	"time"
)

// Keep timestamps per message, independent of later lifecycle/tool events.
type messageCandidate struct {
	text string
	at   time.Time
}

func (c *messageCandidate) consider(text string, at time.Time) {
	if text != "" && (c.text == "" || !at.Before(c.at)) {
		c.text, c.at = text, at
	}
}

func (s *Session) refreshRequest() {
	s.LastRequest = s.lastUser.text
	if s.LastRequest == "" {
		s.LastRequest = s.lastAssistant.text
	}
}

func (s *Session) mergeRequests(other *Session) {
	s.lastUser.consider(other.lastUser.text, other.lastUser.at)
	s.lastAssistant.consider(other.lastAssistant.text, other.lastAssistant.at)
	s.refreshRequest()
}

func (s *Session) readRequest(entry logEntry, at time.Time) {
	var role, text string
	switch entry.Type {
	case "response_item":
		var p struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(entry.Payload, &p) != nil || p.Type != "message" {
			return
		}
		role = p.Role
		var parts []string
		for _, part := range p.Content {
			if part.Type == "input_text" || part.Type == "output_text" || part.Type == "text" {
				if clean := cleanMessage(part.Text); clean != "" {
					parts = append(parts, clean)
				}
			}
		}
		text = strings.Join(parts, " ")
	case "event_msg":
		var p struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Text    string `json:"text"`
		}
		if json.Unmarshal(entry.Payload, &p) != nil {
			return
		}
		switch p.Type {
		case "user_message":
			role = "user"
		case "assistant_message":
			role = "assistant"
		default:
			return
		}
		text = p.Message
		if strings.TrimSpace(text) == "" {
			text = p.Text
		}
		text = cleanMessage(text)
	default:
		return
	}
	switch role {
	case "user":
		s.lastUser.consider(text, at)
	case "assistant":
		s.lastAssistant.consider(text, at)
	default:
		return
	}
	s.refreshRequest()
}

// Remove known injected context while preserving any accompanying request.
// Keep complete cleaned text for search; only the UI shortens the preview.
func cleanMessage(text string) string {
	text = strings.TrimSpace(text)
	for _, tag := range []string{"environment_context", "user_instructions", "instructions", "permissions instructions", "skills_instructions"} {
		open, close := "<"+tag+">", "</"+tag+">"
		for {
			start := strings.Index(text, open)
			if start < 0 {
				break
			}
			end := strings.Index(text[start+len(open):], close)
			if end < 0 {
				text = text[:start]
				break
			}
			end += start + len(open) + len(close)
			text = text[:start] + " " + text[end:]
		}
	}
	text = strings.TrimSpace(text)
	if _, request, ok := strings.Cut(text, "## My request for Codex:"); ok {
		text = strings.TrimSpace(request)
	}
	for _, prefix := range []string{
		"# AGENTS.md instructions for ",
		"<turn_aborted>",
		"A previous agent has worked on this problem",
		"Another language model started to solve this problem",
	} {
		if strings.HasPrefix(text, prefix) {
			return ""
		}
	}
	return strings.Join(strings.Fields(text), " ")
}
