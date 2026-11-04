package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/file_history"
	"github.com/yukino-js/yukino/yukino/internal/logger"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"os"
)

// Port of src/ui/rewind-dialog.tsx and the rewind flow in src/ui/app.tsx.

// formatRewindAgo mirrors formatAgo in src/ui/rewind-dialog.tsx.
// parseSnapshotTime parses the ISO timestamp persisted with a snapshot.
func parseSnapshotTime(value string) time.Time {
	if parsed, err := time.Parse("2006-01-02T15:04:05.000Z", value); err == nil {
		return parsed
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	return time.Time{}
}

func formatRewindAgo(t time.Time) string {
	if t.IsZero() {
		return "0s ago"
	}
	// The reference does not clamp: a future timestamp renders as a negative
	// number of seconds.
	seconds := int64(math.Floor(time.Since(t).Seconds()))
	if seconds < 60 {
		return fmt.Sprintf("%ds ago", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm ago", minutes)
	}
	return fmt.Sprintf("%dh ago", minutes/60)
}

func (m Model) renderRewindDialog() string {
	if m.rewindPhase == 1 {
		return m.renderRewindOptionsDialog()
	}
	return m.renderRewindSnapshotList()
}

func (m Model) renderRewindSnapshotList() string {
	rows := m.dialogAvailableRows()
	chrome := selectorChrome(rows, false)
	visibleCount := max(0, rows-chrome.height)
	windowStart := getListWindowStart(len(m.rewindSnapshots), m.rewindCursor, visibleCount)
	contentWidth := max(1, m.width-2)
	rowWidth := contentWidth
	if contentWidth > 4 {
		rowWidth = contentWidth - 2
	}
	selectedBg := lipgloss.Color(Theme.SelectedBg)

	var content strings.Builder
	for i := windowStart; i < windowStart+visibleCount && i < len(m.rewindSnapshots); i++ {
		snap := m.rewindSnapshots[i]
		selected := i == m.rewindCursor
		label := snap.UserText
		if label == "" {
			label = "(empty)"
		}
		pointer := "  "
		color := normalText
		if selected {
			pointer = "› "
			color = accentColor
		}
		meta := fmt.Sprintf(" · %s · %d files", formatRewindAgo(parseSnapshotTime(snap.Timestamp)), len(snap.Backups))
		labelWidth := rowWidth - visibleWidth(meta)
		var line string
		if labelWidth >= 4 {
			line = lipgloss.NewStyle().Foreground(color).Render(truncateToWidth(pointer+label, labelWidth, "…")) +
				lipgloss.NewStyle().Foreground(mutedText).Render(meta)
		} else {
			line = lipgloss.NewStyle().Foreground(color).Render(truncateToWidth(pointer+label+meta, rowWidth, "…"))
		}
		if selected {
			line = lipgloss.NewStyle().Background(selectedBg).Width(rowWidth).Render(line)
		}
		content.WriteString(" " + line + "\n")
	}

	return renderSelectorFrame(selectorFrameParams{
		title:   "Rewind to checkpoint",
		hint:    "↑↓ navigate · Enter select · Escape cancel",
		content: strings.TrimSuffix(content.String(), "\n"),
		width:   m.width,
		rows:    rows,
	})
}

func (m Model) renderRewindOptionsDialog() string {
	rows := m.dialogAvailableRows()
	snap := m.rewindSnapshots[m.rewindCursor]
	subtitle := snap.UserText
	if subtitle == "" {
		subtitle = "(empty)"
	}
	contentWidth := max(1, m.width-2)
	rowWidth := contentWidth
	if contentWidth > 4 {
		rowWidth = contentWidth - 2
	}
	selectedBg := lipgloss.Color(Theme.SelectedBg)

	var content strings.Builder
	for i, opt := range RestoreOptions {
		selected := i == m.rewindOptionCursor
		pointer := "  "
		color := mutedText
		if selected {
			pointer = Icons.Arrow + " "
			color = accentColor
		}
		line := lipgloss.NewStyle().Foreground(color).Render(truncateToWidth(pointer+opt, rowWidth, "…"))
		if selected {
			line = lipgloss.NewStyle().Background(selectedBg).Width(rowWidth).Render(line)
		}
		content.WriteString(" " + line + "\n")
	}

	return renderSelectorFrame(selectorFrameParams{
		title:    "Choose rewind scope",
		subtitle: subtitle,
		hint:     "↑↓ navigate · Enter select · Escape back",
		content:  strings.TrimSuffix(content.String(), "\n"),
		width:    m.width,
		rows:     rows,
	})
}

func (m Model) handleRewind() (tea.Model, tea.Cmd) {
	if m.fileHistory == nil || !m.fileHistory.HasSnapshots() {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role: "system", content: "No checkpoints to rewind to.",
		})
		return m, nil
	}
	m.rewindSnapshots = m.fileHistory.GetSnapshots()
	m.rewindCursor = len(m.rewindSnapshots) - 1
	m.rewindPhase = 0
	m.rewindOptionCursor = 0
	m.rewindDialog = true
	return m, nil
}

func (m Model) handleRewindKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.rewindPhase == 0 {
		return m.handleRewindPhase0(msg)
	}
	return m.handleRewindPhase1(msg)
}

func (m Model) handleRewindPhase0(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	count := len(m.rewindSnapshots)
	switch msg.String() {
	case "esc":
		m.rewindDialog = false
		return m, nil
	case "up":
		if count > 0 {
			if m.rewindCursor > 0 {
				m.rewindCursor--
			} else {
				m.rewindCursor = count - 1
			}
		}
		return m, nil
	case "down":
		if count > 0 {
			if m.rewindCursor < count-1 {
				m.rewindCursor++
			} else {
				m.rewindCursor = 0
			}
		}
		return m, nil
	case "enter":
		if count > 0 {
			m.rewindPhase = 1
			m.rewindOptionCursor = 0
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleRewindPhase1(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rewindPhase = 0
		return m, nil
	case "up":
		if m.rewindOptionCursor > 0 {
			m.rewindOptionCursor--
		} else {
			m.rewindOptionCursor = len(RestoreOptions) - 1
		}
		return m, nil
	case "down":
		if m.rewindOptionCursor < len(RestoreOptions)-1 {
			m.rewindOptionCursor++
		} else {
			m.rewindOptionCursor = 0
		}
		return m, nil
	case "enter":
		return m.executeRewindOption()
	}
	return m, nil
}

// rewindConversation rewinds the live conversation to a snapshot, persisting
// the rewind. The snapshot's sessionLineCount is the authoritative coordinate:
// the session log is truncated to it and the in-memory conversation is rebuilt
// from the truncated log. Line coordinates survive resume and compaction,
// unlike in-memory message indexes. Snapshots with no recorded
// sessionLineCount — or whose session file is gone — fall back to truncating
// by messageIndex. It returns the visible transcript for the rewound state.
func (m *Model) rewindConversation(snap file_history.Snapshot) []chatMessage {
	wd, _ := os.Getwd()
	sessionFilePath := session.SessionFilePath(wd, m.sessionID)
	if snap.SessionLineCount != nil {
		if _, err := os.Stat(sessionFilePath); err == nil {
			_ = session.TruncateSessionLines(sessionFilePath, *snap.SessionLineCount)
			saved, lerr := session.LoadSession(wd, m.sessionID)
			if lerr == nil {
				m.conversation.Reset()
				m.conversation.AppendMessages(session.RebuildFromSession(saved, false))
				return transcriptFromConversation(session.RebuildFromSession(saved, true))
			}
			// A read failure after truncation falls back to message-index
			// truncation rather than silently rewinding to an empty transcript.
			logger.Global().Error(map[string]any{"err": lerr, "sessionId": m.sessionID}, "rewind reload failed; falling back to message-index truncation")
		}
	}
	m.conversation.TruncateTo(snap.MessageIndex)
	return transcriptFromConversation(m.conversation.GetMessages())
}

// transcriptFromConversation maps the truncated conversation back onto visible
// chat messages after a rewind, dropping system reminders like the reference
// transcript filter.
func transcriptFromConversation(messages []conversation.Message) []chatMessage {
	var out []chatMessage
	for _, msg := range messages {
		if msg.Content == "" {
			continue
		}
		if msg.Role == "system" || strings.HasPrefix(msg.Content, "<system-reminder>") {
			continue
		}
		out = append(out, chatMessage{role: msg.Role, content: msg.Content})
	}
	return out
}

func (m Model) executeRewindOption() (tea.Model, tea.Cmd) {
	snap := m.rewindSnapshots[m.rewindCursor]
	var summary string
	var transcript []chatMessage
	rebuilt := false

	switch m.rewindOptionCursor {
	case 0:
		changed, err := m.fileHistory.Rewind(m.rewindCursor)
		if err != nil {
			return m.finishRewindError(err)
		}
		transcript = m.rewindConversation(snap)
		m.stopMemoryTasks()
		summary = fmt.Sprintf("⟲ Rewound to checkpoint. Restored %d file(s) and conversation.", len(changed))
		for _, f := range changed {
			summary += "\n  " + f
		}
		rebuilt = true

	case 1:
		transcript = m.rewindConversation(snap)
		m.stopMemoryTasks()
		summary = "⟲ Rewound conversation. Files unchanged."
		rebuilt = true

	case 2:
		changed, err := m.fileHistory.Rewind(m.rewindCursor)
		if err != nil {
			return m.finishRewindError(err)
		}
		summary = fmt.Sprintf("⟲ Restored %d file(s). Conversation unchanged.", len(changed))
		for _, f := range changed {
			summary += "\n  " + f
		}

	case 3:
		m.rewindDialog = false
		m.rewindPhase = 0
		return m, nil
	}

	if rebuilt {
		m.pendingMessages = nil
		m.steeringPending = nil
		m.queuePaused = false
		m.chatMessages = transcript
		m.committedUpTo = 0
		// The reference drops the goal manager on a conversation rewind so goal
		// state reloads from the rewound session; rebind the stable agent.
		m.invalidateGoal()
		if m.ag != nil {
			m.ag.GoalManager = m.currentGoal()
		}
	}
	m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: summary})
	m.rewindDialog = false
	m.rewindPhase = 0

	if rebuilt {
		// The reference remounts the Static transcript after a conversation
		// rewind: the screen clears and the restored transcript replays under
		// a fresh header.
		commitText := m.renderTranscriptHeader() + "\n" + m.renderMessagesRange(0, len(m.chatMessages))
		m.committedUpTo = len(m.chatMessages)
		return m, tea.Batch(
			func() tea.Msg { return tea.ClearScreen() },
			tea.Println(commitText),
		)
	}
	commitText := m.renderMessagesRange(m.committedUpTo, len(m.chatMessages))
	m.committedUpTo = len(m.chatMessages)
	if commitText != "" {
		return m, tea.Println(commitText)
	}
	return m, nil
}

func (m Model) finishRewindError(err error) (tea.Model, tea.Cmd) {
	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "error",
		content: fmt.Sprintf("Rewind failed: %s", err),
	})
	m.rewindDialog = false
	m.rewindPhase = 0
	return m, nil
}
