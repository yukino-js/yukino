package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/yukino-js/yukino/yukino/internal/teams"
	"github.com/yukino-js/yukino/yukino/internal/utils"
	"github.com/yukino-js/yukino/yukino/internal/version"
)

type chatMessage struct {
	role          string
	content       string
	toolGroup     []toolBlockInfo
	subAgentBlock *subAgentBlock
	expanded      bool
}

func (m *Model) calcViewportHeight(availableHeight int) int {
	contentLines := m.viewport.TotalLineCount()
	if contentLines < 1 {
		contentLines = 1
	}
	if contentLines > availableHeight {
		return availableHeight
	}
	return contentLines
}

func (m *Model) updateViewport() {
	if !m.ready {
		return
	}
	content := m.renderChatContent()
	m.viewport.SetContent(content)

	statusHeight, sepHeight := 1, 1
	inputHeight := m.editor.Height() + 1
	available := m.height - statusHeight - sepHeight - inputHeight - 1
	if available < 1 {
		available = 1
	}
	m.viewport.Height = m.calcViewportHeight(available)

	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
}

func (m Model) View() string {
	if !m.ready {
		return ""
	}

	switch m.state {
	case stateProviderSelect:
		return m.renderProviderSelectView()
	case stateChat:
		return m.renderChatView()
	}
	return ""
}

func (m Model) renderTeammateTree() string {
	if m.teamMgr == nil {
		return ""
	}
	progressList := m.teamMgr.GetAllTeammateProgress()
	if len(progressList) == 0 {
		return ""
	}

	cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00d7ff"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00"))
	redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00"))

	var sb strings.Builder
	sb.WriteString("\n")

	sb.WriteString("  ┌─ ")
	sb.WriteString(cyanStyle.Render("team-lead"))
	sb.WriteString(": ")
	sb.WriteString(dimStyle.Render("Thinking…"))
	if m.totalInput+m.totalOutput > 0 {
		sb.WriteString(dimStyle.Render(fmt.Sprintf(" · %s tokens", teams.FormatTokens(int64(m.totalInput+m.totalOutput)))))
	}
	sb.WriteString("\n")

	for i, p := range progressList {
		isLast := i == len(progressList)-1
		connector := "  ├─ "
		if isLast {
			connector = "  └─ "
		}

		sb.WriteString(connector)
		sb.WriteString(cyanStyle.Render("@" + p.Name))
		sb.WriteString(": ")

		status := p.GetStatus()
		switch status {
		case "completed":
			sb.WriteString(greenStyle.Render("completed"))
		case "failed":
			sb.WriteString(redStyle.Render("failed"))
		case "stopped":
			sb.WriteString(yellowStyle.Render("stopped"))
		case "idle":
			sb.WriteString(dimStyle.Render("idle"))
		default:
			sb.WriteString(dimStyle.Render(p.ActivitySummary() + "..."))
		}

		stats := fmt.Sprintf(" · %d tools · %s tokens", p.GetToolUseCount(), teams.FormatTokens(p.GetTokenCount()))
		sb.WriteString(dimStyle.Render(stats))
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderActiveDialog mirrors the InteractionDock priority in
// src/ui/interaction-dock.tsx: the first active dialog replaces the composer.
func (m Model) renderActiveDialog() string {
	switch {
	case m.loginDialog:
		return m.renderLoginDialog()
	case m.providerDialog:
		return m.renderProviderDialog()
	case m.modelDialog:
		return m.renderModelDialog()
	case m.planApprovalDialog:
		return m.renderPlanApprovalDialog()
	case m.rewindDialog:
		return m.renderRewindDialog()
	case m.resumeDialog:
		return m.renderResumeDialog()
	case m.permDialog:
		return m.renderPermDialog()
	case m.askUserDialog:
		return m.renderAskUserDialog()
	case m.sandboxDialog:
		return m.renderSandboxDialog()
	}
	return ""
}

func (m Model) renderChatView() string {
	footerRows, statusRows, dockRows := m.terminalBudgets()
	footer := m.renderFooter(footerRows)
	status := m.renderUIStatus(statusRows)
	dock := strings.TrimSuffix(m.renderActiveDialog(), "\n")
	if dock == "" {
		dock = m.renderComposer(dockRows)
	}
	dockLines := strings.Split(dock, "\n")
	if len(dockLines) > dockRows {
		dock = strings.Join(dockLines[:dockRows], "\n")
	}
	activityRows := max(0, dockRows-lineCount(dock))
	activity := strings.TrimSuffix(m.renderChatContent(), "\n")
	parts := []string{}
	if activity != "" && activityRows > 0 {
		lines := strings.Split(activity, "\n")
		parts = append(parts, strings.Join(lines[max(0, len(lines)-activityRows):], "\n"))
	}
	if m.height-1 >= 4 {
		parts = append(parts, "")
	}
	if status != "" {
		parts = append(parts, status)
	}
	parts = append(parts, dock)
	if footer != "" {
		parts = append(parts, footer)
	}
	return strings.Join(parts, "\n")
}

// renderTranscriptHeader mirrors the Static transcript header in
// src/ui/transcript.tsx; it is reprinted whenever the scrollback is reset
// (startup, /clear, rewind, resume).
func (m Model) renderTranscriptHeader() string {
	padding := 0
	if m.width > 2 {
		padding = 1
	}
	width := max(1, m.width-padding*2)
	hint := "/help · Esc interrupt"
	if m.width >= 80 {
		hint = "Esc interrupt · /commands · Ctrl+O details"
	} else if m.width >= 40 {
		hint = "/help · Esc interrupt · Ctrl+O details"
	}
	provider := ""
	model := ""
	if m.selectedProvider != nil {
		provider = m.selectedProvider.Name
		model = m.selectedProvider.Model
		if model == "" {
			model = provider
		}
	}
	pad := strings.Repeat(" ", padding)
	title := lipgloss.NewStyle().Foreground(accentColor).Render("✳ ") +
		lipgloss.NewStyle().Bold(true).Foreground(normalText).Render("Yukino") +
		lipgloss.NewStyle().Foreground(dimText).Render(" v"+version.Version)
	line2 := lipgloss.NewStyle().Foreground(mutedText).Render(truncateToWidth(hint, width, "…"))
	line3 := lipgloss.NewStyle().Foreground(dimText).Render(truncateToWidth(
		plainTerminalLine(fmt.Sprintf("%s/%s · %s", provider, model, utils.CompactPath(m.getWorkDir()))), width, "…"))
	return "\n" + pad + title + "\n" + pad + line2 + "\n" + pad + line3
}

func (m Model) renderChatContent() string {
	var sb strings.Builder

	for _, msg := range m.chatMessages[m.committedUpTo:] {
		switch msg.role {
		case "user":
			sb.WriteString(promptStyle.Render("❯ "))
			sb.WriteString(lipgloss.NewStyle().Foreground(brightText).Bold(true).Render(msg.content))
			sb.WriteString("\n\n")

		case "assistant":
			sb.WriteString(aiMarkerStyle.Render("● "))
			rendered := m.renderMarkdown(msg.content)
			indented := indentBlock(rendered, "  ")
			sb.WriteString(strings.TrimLeft(indented, " "))
			sb.WriteString("\n\n")

		case "tool", "tool_visible":
			sb.WriteString("  ")
			if strings.HasPrefix(msg.content, "✗") {
				sb.WriteString(toolErrorStyle.Render(msg.content))
			} else {
				sb.WriteString(toolDoneStyle.Render(msg.content))
			}
			sb.WriteString("\n")
			appendEditDiff(&sb, msg.toolGroup)

		case "tool_collapsed":
			if msg.expanded {
				for _, tb := range msg.toolGroup {
					sb.WriteString("  ")
					text := renderToolBlockText(tb)
					if tb.isError {
						sb.WriteString(toolErrorStyle.Render(text))
					} else {
						sb.WriteString(toolDoneStyle.Render(text))
					}
					sb.WriteString("\n")
				}
			}

		case "tool_group":
			if msg.expanded {
				for _, tb := range msg.toolGroup {
					sb.WriteString("  ")
					text := renderToolBlockText(tb)
					if tb.isError {
						sb.WriteString(toolErrorStyle.Render(text))
					} else {
						sb.WriteString(toolDoneStyle.Render(text))
					}
					sb.WriteString("\n")
				}
			} else {
				summary := renderToolGroupSummary(msg.toolGroup)
				sb.WriteString(toolDoneStyle.Render("  " + summary))
				sb.WriteString(lipgloss.NewStyle().Foreground(dimText).Render("  (ctrl+o to expand)"))
				sb.WriteString("\n")
			}

		case "sub_agent":
			if msg.subAgentBlock != nil {
				sb.WriteString(renderSubAgentBlock(msg.subAgentBlock, msg.expanded))
			}

		case "thinking":
			sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(2).Render(msg.content))
			sb.WriteString("\n\n")

		case "system":
			sb.WriteString(lipgloss.NewStyle().Foreground(dimText).PaddingLeft(2).Render(msg.content))
			sb.WriteString("\n\n")

		case "error":
			sb.WriteString(errorStyle.Render("✖ " + msg.content))
			sb.WriteString("\n\n")
		}
	}

	if m.activeSubAgent != nil && !m.activeSubAgent.done {
		sb.WriteString(renderSubAgentBlock(m.activeSubAgent, false))
	}

	for _, tb := range m.toolBlocks {
		if tb.toolName == "Agent" && m.activeSubAgent != nil {
			continue
		}
		sb.WriteString(m.renderToolBlock(tb))
		sb.WriteString("\n")
	}

	if m.streaming && m.streamBuf != "" {
		sb.WriteString(aiMarkerStyle.Render("● "))
		indented := indentBlock(m.streamBuf, "  ")
		sb.WriteString(streamingTextStyle.Render(strings.TrimLeft(indented, " ")))
		sb.WriteString("\n")
	}

	if m.streaming {
		elapsed := time.Since(m.thinkingStart).Seconds()
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(brandPurple).Render(
			fmt.Sprintf("  %s Thinking…  (%.0fs)", m.spinner.View(), elapsed),
		))
		sb.WriteString("\n")
		sb.WriteString(m.renderTeammateTree())
	}

	if !m.streaming {
		tree := m.renderTeammateTree()
		if tree != "" {
			sb.WriteString(tree)
		}
	}

	return sb.String()
}

func (m Model) renderMessagesRange(from, to int) string {
	from = max(0, min(from, len(m.chatMessages)))
	to = max(from, min(to, len(m.chatMessages)))
	m.chatMessages = m.chatMessages[from:to]
	m.committedUpTo = 0
	m.streaming = false
	m.toolBlocks = nil
	m.activeSubAgent = nil
	m.teamMgr = nil
	return m.renderChatContent()
}

func (m Model) getModelName() string {
	if m.selectedProvider != nil {
		return m.selectedProvider.Model
	}
	if len(m.providers) > 0 {
		return m.providers[0].Model
	}
	return "unknown"
}

func (m Model) getWorkDir() string {
	wd, _ := os.Getwd()
	return wd
}
