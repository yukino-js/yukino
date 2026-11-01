package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
)

func (m Model) handleResume(args string) (tea.Model, tea.Cmd) {
	wd, _ := os.Getwd()
	sessions := session.ListSessions(wd)

	if args != "" {
		return m.doResumeSession(wd, args, sessions)
	}

	if len(sessions) == 0 {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role: "system", content: "No previous sessions found.",
		})
		m.updateViewport()
		return m, nil
	}

	m.resumeSessions = sessions
	m.resumeFiltered = sessions
	m.resumeCursor = 0
	m.resumeSearch = ""
	m.resumeScrollTop = 0
	m.state = stateResume
	return m, nil
}

func (m Model) doResumeSession(wd, targetID string, sessions []session.SessionInfo) (tea.Model, tea.Cmd) {
	var idx int
	if n, _ := fmt.Sscanf(targetID, "%d", &idx); n == 1 && idx >= 1 && idx <= len(sessions) {
		targetID = sessions[idx-1].ID
	}

	msgs := session.LoadSession(wd, strings.TrimSpace(targetID))
	if len(msgs) == 0 {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role: "error", content: fmt.Sprintf("Session '%s' not found or empty.", targetID),
		})
		m.updateViewport()
		return m, nil
	}

	m.chatMessages = nil
	m.committedUpTo = 0
	m.conversation = conversation.NewManager()
	m.sessionID = strings.TrimSpace(targetID)
	if m.ag != nil {
		m.ag.SetSessionID(m.sessionID)
	}

	boundary, after, compacted := session.FindLastCompactBoundary(msgs)
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
		replay = msgs
	}

	for _, msg := range replay {
		if msg.Content != "" {
			m.chatMessages = append(m.chatMessages, chatMessage{role: msg.Role, content: msg.Content})
		}
		m.conversation.AppendMessages([]conversation.Message{msg.ToConversation()})
	}

	restored := fmt.Sprintf("Session %s restored (%d messages).", strings.TrimSpace(targetID), len(replay))
	if compacted {
		restored = fmt.Sprintf("Session %s restored from compacted state (summary + %d kept + %d newer messages).",
			strings.TrimSpace(targetID), len(boundary.Keep), len(after))
	}
	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "system",
		content: restored,
	})
	commitText := m.renderMessagesRange(0, len(m.chatMessages))
	m.committedUpTo = len(m.chatMessages)
	m.updateViewport()
	if commitText != "" {
		return m, tea.Println(commitText)
	}
	return m, nil
}

func (m Model) handleResumeKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = stateChat
		m.textarea.Focus()
		return m, nil
	case "enter":
		if m.resumeCursor < len(m.resumeFiltered) {
			selected := m.resumeFiltered[m.resumeCursor]
			m.state = stateChat
			m.textarea.Focus()
			wd, _ := os.Getwd()
			return m.doResumeSession(wd, selected.ID, m.resumeSessions)
		}
		return m, nil
	case "up":
		if m.resumeCursor > 0 {
			m.resumeCursor--
			if m.resumeCursor < m.resumeScrollTop {
				m.resumeScrollTop = m.resumeCursor
			}
		}
		return m, nil
	case "down":
		if m.resumeCursor < len(m.resumeFiltered)-1 {
			m.resumeCursor++
			maxVisible := m.resumeVisibleCount()
			if m.resumeCursor >= m.resumeScrollTop+maxVisible {
				m.resumeScrollTop = m.resumeCursor - maxVisible + 1
			}
		}
		return m, nil
	case "backspace":
		if len(m.resumeSearch) > 0 {
			m.resumeSearch = m.resumeSearch[:len(m.resumeSearch)-1]
			m.resumeFilterSessions()
		}
		return m, nil
	default:
		if len(msg.String()) == 1 && msg.String() >= " " {
			m.resumeSearch += msg.String()
			m.resumeFilterSessions()
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) resumeFilterSessions() {
	m.resumeFiltered = nil
	for _, s := range m.resumeSessions {
		if session.MatchesSearch(s, m.resumeSearch) {
			m.resumeFiltered = append(m.resumeFiltered, s)
		}
	}
	m.resumeCursor = 0
	m.resumeScrollTop = 0
}

func (m Model) resumeVisibleCount() int {
	available := m.height - 8
	perItem := 2
	if available < perItem {
		return 1
	}
	return available / perItem
}

func (m Model) renderResumeView() string {
	var sb strings.Builder

	total := len(m.resumeFiltered)
	current := 0
	if total > 0 {
		current = m.resumeCursor + 1
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(2).Render(
		fmt.Sprintf("Resume session (%d of %d)", current, total),
	))
	sb.WriteString("\n")

	searchText := m.resumeSearch
	if searchText == "" {
		searchText = lipgloss.NewStyle().Foreground(dimText).Render("⌕ Search…")
	} else {
		searchText = "⌕ " + searchText
	}
	boxWidth := m.width - 6
	if boxWidth < 20 {
		boxWidth = 20
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(boxWidth).
		PaddingLeft(1)
	sb.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(border.Render(searchText)))
	sb.WriteString("\n")

	wd, _ := os.Getwd()
	projectName := filepath.Base(wd)
	sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(4).Render(projectName))
	sb.WriteString("\n\n")

	maxVisible := m.resumeVisibleCount()
	if maxVisible > total {
		maxVisible = total
	}

	for i := m.resumeScrollTop; i < m.resumeScrollTop+maxVisible && i < total; i++ {
		s := m.resumeFiltered[i]

		title := s.FirstMessage
		if title == "" {
			title = "(empty session)"
		}
		maxTitleLen := m.width - 8
		if maxTitleLen > 0 && len(title) > maxTitleLen {
			title = title[:maxTitleLen] + "…"
		}

		prefix := "  "
		if i == m.resumeCursor {
			prefix = "❯ "
			title = lipgloss.NewStyle().Foreground(cyanText).Bold(true).Render(title)
		} else {
			title = lipgloss.NewStyle().Foreground(normalText).Render(title)
		}
		sb.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(prefix + title))
		sb.WriteString("\n")

		var meta []string
		meta = append(meta, session.FormatRelativeTime(s.ModTime))
		if s.GitBranch != "" {
			meta = append(meta, s.GitBranch)
		}
		meta = append(meta, session.FormatFileSize(s.FileSize))
		metaStr := strings.Join(meta, " · ")
		sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(6).Render(metaStr))
		sb.WriteString("\n")

		if i < m.resumeScrollTop+maxVisible-1 && i < total-1 {
			sb.WriteString("\n")
		}
	}

	if total > maxVisible {
		if m.resumeScrollTop+maxVisible < total {
			sb.WriteString("\n")
			sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(2).Render(
				fmt.Sprintf("  ↓ %d more session(s)", total-m.resumeScrollTop-maxVisible),
			))
		}
	}

	rendered := strings.Count(sb.String(), "\n") + 1
	footerHeight := 2
	pad := m.height - rendered - footerHeight
	if pad > 0 {
		sb.WriteString(strings.Repeat("\n", pad))
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(4).Render(
		"Type to search · Enter to select · Esc to cancel",
	))

	return sb.String()
}
