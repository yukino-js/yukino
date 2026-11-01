package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderRewindDialog() string {
	if m.rewindPhase == 1 {
		return m.renderRewindOptionsDialog()
	}
	return m.renderRewindSnapshotList()
}

func (m Model) renderRewindSnapshotList() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(brandPurple).Bold(true).Render("  ⟲ Rewind to checkpoint"))
	sb.WriteString("\n\n")

	maxVisible := 8
	start := 0
	if m.rewindCursor >= maxVisible {
		start = m.rewindCursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.rewindSnapshots) {
		end = len(m.rewindSnapshots)
	}

	for i := start; i < end; i++ {
		snap := m.rewindSnapshots[i]
		prefix := "   "
		style := lipgloss.NewStyle().Foreground(dimText)
		if i == m.rewindCursor {
			prefix = " ❯ "
			style = lipgloss.NewStyle().Foreground(cyanText)
		}
		ago := time.Since(snap.Timestamp).Truncate(time.Second)
		label := snap.UserText
		if len(label) > 50 {
			label = label[:50] + "…"
		}
		files := len(snap.Backups)
		line := fmt.Sprintf("%s[%d] %s (%s ago, %d file(s))", prefix, i+1, label, ago, files)
		sb.WriteString(style.Render(line) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(2).Render("↑/↓ navigate · enter select · esc cancel"))
	sb.WriteString("\n")
	return sb.String()
}

func (m Model) renderRewindOptionsDialog() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(brandPurple).Bold(true).Render("  ⟲ Rewind to checkpoint"))
	sb.WriteString("\n\n")

	snap := m.rewindSnapshots[m.rewindCursor]
	ago := time.Since(snap.Timestamp).Truncate(time.Second)
	label := snap.UserText
	if len(label) > 50 {
		label = label[:50] + "…"
	}
	selected := fmt.Sprintf("  Selected: [%d] %s (%s ago, %d file(s))", m.rewindCursor+1, label, ago, len(snap.Backups))
	sb.WriteString(lipgloss.NewStyle().Foreground(normalText).Render(selected))
	sb.WriteString("\n\n")

	for i, opt := range RestoreOptions {
		prefix := "   "
		style := lipgloss.NewStyle().Foreground(dimText)
		if i == m.rewindOptionCursor {
			prefix = " ❯ "
			style = lipgloss.NewStyle().Foreground(cyanText)
		}
		sb.WriteString(style.Render(prefix+opt) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(2).Render("↑/↓ navigate · enter select · esc back"))
	sb.WriteString("\n")
	return sb.String()
}

func (m Model) handleRewind() (tea.Model, tea.Cmd) {
	if m.fileHistory == nil || !m.fileHistory.HasSnapshots() {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role: "system", content: "No checkpoints to rewind to.",
		})
		m.updateViewport()
		return m, nil
	}
	m.rewindSnapshots = m.fileHistory.GetSnapshots()
	m.rewindCursor = len(m.rewindSnapshots) - 1
	m.rewindPhase = 0
	m.rewindOptionCursor = 0
	m.rewindDialog = true
	m.updateViewport()
	return m, nil
}

func (m Model) handleRewindKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.rewindPhase == 0 {
		return m.handleRewindPhase0(msg)
	}
	return m.handleRewindPhase1(msg)
}

func (m Model) handleRewindPhase0(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rewindDialog = false
		m.textarea.Focus()
		m.updateViewport()
		return m, nil
	case "up":
		if m.rewindCursor > 0 {
			m.rewindCursor--
			m.updateViewport()
		}
		return m, nil
	case "down":
		if m.rewindCursor < len(m.rewindSnapshots)-1 {
			m.rewindCursor++
			m.updateViewport()
		}
		return m, nil
	case "enter":
		if m.rewindCursor < len(m.rewindSnapshots) {
			m.rewindPhase = 1
			m.rewindOptionCursor = 0
			m.updateViewport()
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleRewindPhase1(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rewindPhase = 0
		m.updateViewport()
		return m, nil
	case "up":
		if m.rewindOptionCursor > 0 {
			m.rewindOptionCursor--
			m.updateViewport()
		}
		return m, nil
	case "down":
		if m.rewindOptionCursor < len(RestoreOptions)-1 {
			m.rewindOptionCursor++
			m.updateViewport()
		}
		return m, nil
	case "enter":
		return m.executeRewindOption()
	}
	return m, nil
}

func (m Model) executeRewindOption() (tea.Model, tea.Cmd) {
	snap := m.rewindSnapshots[m.rewindCursor]
	var summary string

	switch m.rewindOptionCursor {
	case 0:
		changed, err := m.fileHistory.Rewind(m.rewindCursor)
		if err != nil {
			m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: fmt.Sprintf("Rewind failed: %s", err)})
			m.rewindDialog = false
			m.textarea.Focus()
			m.updateViewport()
			return m, nil
		}
		m.conversation.TruncateTo(snap.MessageIndex)
		summary = fmt.Sprintf("⟲ Rewound to checkpoint. Restored %d file(s) and conversation.", len(changed))
		for _, f := range changed {
			summary += "\n  " + f
		}
		m.chatMessages = m.chatMessages[:0]
		m.committedUpTo = 0

	case 1:
		m.conversation.TruncateTo(snap.MessageIndex)
		summary = "⟲ Rewound conversation. Files unchanged."
		m.chatMessages = m.chatMessages[:0]
		m.committedUpTo = 0

	case 2:
		changed, err := m.fileHistory.Rewind(m.rewindCursor)
		if err != nil {
			m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: fmt.Sprintf("Rewind failed: %s", err)})
			m.rewindDialog = false
			m.textarea.Focus()
			m.updateViewport()
			return m, nil
		}
		summary = fmt.Sprintf("⟲ Restored %d file(s). Conversation unchanged.", len(changed))
		for _, f := range changed {
			summary += "\n  " + f
		}

	case 3:
		m.rewindDialog = false
		m.rewindPhase = 0
		m.textarea.Focus()
		m.updateViewport()
		return m, nil
	}

	m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: summary})
	m.rewindDialog = false
	m.rewindPhase = 0
	m.textarea.Focus()
	m.updateViewport()

	if m.rewindOptionCursor <= 1 {
		return m, tea.Batch(
			func() tea.Msg { return tea.ClearScreen() },
			tea.Println(lipgloss.NewStyle().Foreground(dimText).Render(summary)),
		)
	}
	commitText := m.renderMessagesRange(m.committedUpTo-1, len(m.chatMessages))
	m.committedUpTo = len(m.chatMessages)
	if commitText != "" {
		return m, tea.Println(commitText)
	}
	return m, nil
}
