package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/file_history"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/todo"
)

// Port of src/ui/session-selector.tsx and the /resume command flow in
// src/ui/app.tsx.

func sessionFuseKeys() []fuseKey[session.SessionInfo] {
	return []fuseKey[session.SessionInfo]{
		{weight: 1, get: func(s session.SessionInfo) (string, bool) { return s.ID, true }},
		{weight: 1, get: func(s session.SessionInfo) (string, bool) { return s.FirstMessage, true }},
	}
}

func listResumableSessions(wd string) []session.SessionInfo {
	var sessions []session.SessionInfo
	for _, s := range session.ListSessions(wd) {
		if s.MessageCount > 0 {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

func (m Model) handleResume(args string) (tea.Model, tea.Cmd) {
	wd, _ := os.Getwd()
	arg := strings.TrimSpace(args)
	if arg == "" {
		sessions := listResumableSessions(wd)
		if len(sessions) == 0 {
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "No sessions found."})
			m.updateViewport()
			return m, nil
		}
		return m.openResumeDialog(sessions)
	}

	saved := session.LoadSessionForResume(wd, arg)
	if len(saved) == 0 {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "system",
			content: fmt.Sprintf("Session %q not found or empty.", arg),
		})
		sessions := listResumableSessions(wd)
		if len(sessions) > 0 {
			return m.openResumeDialog(sessions)
		}
		m.updateViewport()
		return m, nil
	}
	return m.doResumeSession(wd, arg, saved)
}

func (m Model) openResumeDialog(sessions []session.SessionInfo) (tea.Model, tea.Cmd) {
	m.resumeSessions = sessions
	m.resumeQuery = ""
	m.resumeFocusedID = m.sessionID
	m.resumeFocusSet = true
	m.resumeDialog = true
	m.updateViewport()
	return m, nil
}

func (m Model) resumeMatches() []session.SessionInfo {
	query := strings.TrimSpace(m.resumeQuery)
	if query == "" {
		return m.resumeSessions
	}
	return NewFuse(m.resumeSessions, sessionFuseKeys(), 0.35, true).Search(query)
}

func (m Model) resumeCursor(matches []session.SessionInfo) int {
	if m.resumeFocusSet {
		for i, s := range matches {
			if s.ID == m.resumeFocusedID {
				return i
			}
		}
	}
	return 0
}

func (m Model) handleResumeDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := m.resumeMatches()
	switch msg.String() {
	case "esc":
		m.resumeDialog = false
		m.updateViewport()
		return m, nil
	case "up", "down":
		if len(matches) > 0 {
			cursor := m.resumeCursor(matches)
			next := cursor - 1
			if msg.String() == "down" {
				next = cursor + 1
			}
			next = (next + len(matches)) % len(matches)
			m.resumeFocusedID = matches[next].ID
			m.resumeFocusSet = true
		}
		m.updateViewport()
		return m, nil
	case "enter":
		if len(matches) > 0 {
			selected := matches[m.resumeCursor(matches)]
			m.resumeDialog = false
			wd, _ := os.Getwd()
			return m.doResumeSession(wd, selected.ID, nil)
		}
		return m, nil
	default:
		nextQuery := updateSelectorQuery(m.resumeQuery, msg)
		if nextQuery != m.resumeQuery {
			m.resumeQuery = nextQuery
			if strings.TrimSpace(nextQuery) != "" {
				m.resumeFocusSet = false
			} else {
				m.resumeFocusedID = m.sessionID
				m.resumeFocusSet = true
			}
		}
		m.updateViewport()
		return m, nil
	}
}

// formatSessionAgo mirrors formatRelativeTime in src/ui/session-selector.tsx.
func formatSessionAgo(t time.Time) string {
	seconds := int64(time.Since(t).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds ago", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm ago", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%dh ago", seconds/3600)
	default:
		return fmt.Sprintf("%dd ago", seconds/86400)
	}
}

func (m Model) renderResumeDialog() string {
	if !m.resumeDialog {
		return ""
	}
	matches := m.resumeMatches()
	cursor := m.resumeCursor(matches)
	emptyText := "No saved sessions"
	if strings.TrimSpace(m.resumeQuery) != "" {
		emptyText = "No matching sessions"
	}
	return renderSelectorList(selectorListParams{
		title:        "Resume session",
		hint:         "↑↓ navigate · Enter resume · Esc cancel · Ctrl+U clear",
		query:        m.resumeQuery,
		emptyText:    emptyText,
		itemCount:    len(matches),
		itemHeight:   2,
		totalCount:   len(m.resumeSessions),
		cursor:       cursor,
		searchActive: true,
		width:        m.width,
		rows:         m.dialogAvailableRows(),
		renderRows: func(start, count, width int) string {
			var sb strings.Builder
			for i := start; i < start+count && i < len(matches); i++ {
				s := matches[i]
				label := s.FirstMessage
				if label == "" {
					label = "(empty session)"
				}
				activity := "no user messages"
				if s.LastUserMessageTime != nil {
					activity = formatSessionAgo(*s.LastUserMessageTime)
				}
				sb.WriteString(renderSelectorListRow(selectorListRowParams{
					label:     label,
					detail:    fmt.Sprintf("%s · %d messages · %s", s.ID, s.MessageCount, activity),
					hasDetail: true,
					focused:   i == cursor,
					current:   s.ID == m.sessionID,
					width:     width,
				}))
			}
			return sb.String()
		},
	})
}

// doResumeSession rebuilds the conversation from the saved transcript and
// continues under the resumed session id. `saved` may be nil to load fresh.
func (m Model) doResumeSession(wd, targetID string, saved []session.Message) (tea.Model, tea.Cmd) {
	if saved == nil {
		saved = session.LoadSessionForResume(wd, targetID)
	}
	if len(saved) == 0 {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "error",
			content: fmt.Sprintf("Session %q not found or empty.", targetID),
		})
		m.updateViewport()
		return m, nil
	}

	m.chatMessages = nil
	m.committedUpTo = 0
	m.conversation = conversation.NewManager()
	m.sessionID = targetID

	// Re-key the session-derived stores to the resumed session.
	m.fileHistory = file_history.New(m.sessionID)
	m.defaultTools.EditFile.FileHistory = m.fileHistory
	m.defaultTools.WriteFile.FileHistory = m.fileHistory
	if store, err := todo.NewStore(m.sessionID); err == nil {
		m.todoList = todo.NewTaskList(store)
	}
	if m.ag != nil {
		m.ag.SetSessionID(m.sessionID)
		m.ag.FileHistory = m.fileHistory
		if m.ag.Checker != nil {
			m.ag.Checker.PlanFilePath = ""
		}
	}

	boundary, after, compacted := session.FindLastCompactBoundary(saved)
	var replay []session.Message
	if compacted {
		resumeSummary := fmt.Sprintf("The conversation history before this point was compacted into the following summary:\n\n<summary>\n%s\n</summary>", boundary.Summary)
		if len(boundary.Keep) > 0 {
			resumeSummary += "\n\nRecent messages have been preserved verbatim."
		}
		replay = append(replay, session.Message{Role: "user", Content: resumeSummary})
		for _, k := range boundary.Keep {
			replay = append(replay, session.Message{
				Role:        k.Role,
				Content:     k.Content,
				ToolUses:    k.ToolUses,
				ToolResults: k.ToolResults,
			})
		}
		replay = append(replay, after...)
	} else {
		replay = saved
	}

	for _, msg := range replay {
		if msg.Content != "" {
			m.chatMessages = append(m.chatMessages, chatMessage{role: msg.Role, content: msg.Content})
		}
		m.conversation.AppendMessages([]conversation.Message{msg.ToConversation()})
	}

	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "system",
		content: fmt.Sprintf("⟲ Resumed session %s (%d messages).", targetID, len(replay)),
	})
	// Resuming remounts the transcript in the reference (the Static key
	// changes with the session id): clear the screen and replay under a
	// fresh header.
	commitText := m.renderTranscriptHeader() + "\n" + m.renderMessagesRange(0, len(m.chatMessages))
	m.committedUpTo = len(m.chatMessages)
	m.textarea.Focus()
	m.updateViewport()
	return m, tea.Batch(
		func() tea.Msg { return tea.ClearScreen() },
		tea.Println(commitText),
	)
}
