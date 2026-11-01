package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
)

var permOptions = []struct {
	label    string
	response agent.PermissionResponse
}{
	{PermissionOptions[0].Label, agent.PermAllow},
	{PermissionOptions[1].Label, agent.PermAllowAlways},
	{PermissionOptions[2].Label, agent.PermDeny},
}

var permDenyIdx = len(permOptions) - 1

func (m Model) handlePermDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.permCursor > 0 {
			m.permCursor--
		}
		return m, nil
	case "down":
		if m.permCursor < len(permOptions)-1 {
			m.permCursor++
		}
		return m, nil
	case "enter":
		reply := agent.PermissionReply{Response: permOptions[m.permCursor].response}
		if m.permCursor == permDenyIdx {
			reply.Feedback = m.permFeedback
		}
		return m.replyPermission(reply)
	case "esc":
		return m.replyPermission(agent.PermissionReply{Response: agent.PermDeny})
	case "backspace":
		if m.permCursor == permDenyIdx && len(m.permFeedback) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.permFeedback)
			m.permFeedback = m.permFeedback[:len(m.permFeedback)-size]
		}
		return m, nil
	default:
		if m.permCursor == permDenyIdx && len(msg.Runes) > 0 {
			m.permFeedback += string(msg.Runes)
		}
		return m, nil
	}
}

func (m Model) replyPermission(reply agent.PermissionReply) (tea.Model, tea.Cmd) {
	m.permRespCh <- reply
	m.closePermDialog()
	m.updateViewport()
	return m, m.listenForAgentEvents()
}

func (m *Model) closePermDialog() {
	m.permDialog = false
	m.permCursor = 0
	m.permFeedback = ""
	m.permRespCh = nil
}

func (m Model) renderPermDialog() string {
	var sb strings.Builder

	sb.WriteString(permBorderStyle.Render(fmt.Sprintf("  %s command", m.permToolName)))
	sb.WriteString("\n\n")

	desc := m.permDesc
	if desc != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(normalText).PaddingLeft(4).Render(desc))
		sb.WriteString("\n\n")
	}

	sb.WriteString(permDimStyle.Render("  This command requires approval"))
	sb.WriteString("\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(normalText).PaddingLeft(2).Render("Do you want to proceed?"))
	sb.WriteString("\n")

	for i, opt := range permOptions {
		prefix := "   "
		style := lipgloss.NewStyle().Foreground(dimText)
		if i == m.permCursor {
			prefix = " ❯ "
			style = lipgloss.NewStyle().Foreground(cyanText)
		}
		label := fmt.Sprintf("%d. %s", i+1, opt.label)
		line := style.Render(prefix + label)
		if i == permDenyIdx && i == m.permCursor {
			if m.permFeedback == "" {
				line += style.Render(", ") + lipgloss.NewStyle().Foreground(dimText).Render("and tell Yukino what to do differently█")
			} else {
				line += style.Render(", " + m.permFeedback + "█")
			}
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString(permDimStyle.Render("  Esc to cancel"))
	sb.WriteString("\n")

	sb.WriteString("\n")
	return sb.String()
}

func nextPermissionMode(current permissions.PermissionMode) permissions.PermissionMode {
	switch current {
	case permissions.ModeDefault:
		return permissions.ModeAcceptEdits
	case permissions.ModeAcceptEdits:
		return permissions.ModePlan
	case permissions.ModePlan:
		return permissions.ModeBypass
	case permissions.ModeBypass:
		return permissions.ModeDefault
	default:
		return permissions.ModeDefault
	}
}

func permissionModeInfo(mode permissions.PermissionMode) (string, string) {
	switch mode {
	case permissions.ModeDefault:
		return "Default", "Writes and commands require approval."
	case permissions.ModeAcceptEdits:
		return "Accept Edits", "File edits auto-approved; commands still require approval."
	case permissions.ModePlan:
		return "Plan", "Read-only mode. No writes or commands executed."
	case permissions.ModeBypass:
		return "YOLO", "All tools auto-approved. Use with caution."
	default:
		return string(mode), ""
	}
}
