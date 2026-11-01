package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/commands"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/sandbox"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

func (m Model) handleSandboxDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	labels := commands.SandboxModeLabels()
	switch msg.String() {
	case "up", "k":
		if m.sandboxCursor > 0 {
			m.sandboxCursor--
		}
		m.updateViewport()
		return m, nil
	case "down", "j":
		if m.sandboxCursor < len(labels)-1 {
			m.sandboxCursor++
		}
		m.updateViewport()
		return m, nil
	case "enter":
		m.sandboxDialog = false
		mode := commands.SandboxMode(m.sandboxCursor)
		return m.applySandboxMode(mode)
	case "esc":
		m.sandboxDialog = false
		m.updateViewport()
		return m, nil
	}
	return m, nil
}

func (m Model) applySandboxMode(mode commands.SandboxMode) (tea.Model, tea.Cmd) {
	bashTool, _ := m.registry.Get("Bash").(*tools.BashTool)
	labels := commands.SandboxModeLabels()
	descriptions := commands.SandboxModeDescriptions()

	switch mode {
	case commands.SandboxAutoAllow:
		sb := sandbox.New()
		if bashTool != nil {
			bashTool.Sandbox = sb
			if m.ag != nil && m.ag.Checker != nil && m.ag.Checker.Sandbox != nil {
				bashTool.SandboxConfig = sandbox.Config{
					AllowWrite:     m.ag.Checker.Sandbox.GetAllowedRoots(),
					DenyWrite:      nil,
					NetworkEnabled: false,
				}
			}
		}
		if m.ag != nil && m.ag.Checker != nil {
			m.ag.Checker.SandboxEnabled = true
			m.ag.Checker.SandboxAutoAllow = true
		}
	case commands.SandboxRegular:
		sb := sandbox.New()
		if bashTool != nil {
			bashTool.Sandbox = sb
			if m.ag != nil && m.ag.Checker != nil && m.ag.Checker.Sandbox != nil {
				bashTool.SandboxConfig = sandbox.Config{
					AllowWrite:     m.ag.Checker.Sandbox.GetAllowedRoots(),
					DenyWrite:      nil,
					NetworkEnabled: false,
				}
			}
		}
		if m.ag != nil && m.ag.Checker != nil {
			m.ag.Checker.SandboxEnabled = true
			m.ag.Checker.SandboxAutoAllow = false
		}
	case commands.SandboxOff:
		if bashTool != nil {
			bashTool.Sandbox = nil
			bashTool.SandboxConfig = sandbox.Config{}
		}
		if m.ag != nil && m.ag.Checker != nil {
			m.ag.Checker.SandboxEnabled = false
			m.ag.Checker.SandboxAutoAllow = false
		}
	}

	msg := fmt.Sprintf("Sandbox mode switched: %s\n%s", labels[mode], descriptions[mode])
	m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: msg})
	m.updateViewport()
	return m, nil
}

func (m Model) renderSandboxDialog() string {
	if !m.sandboxDialog {
		return ""
	}
	var sb strings.Builder

	header := lipgloss.NewStyle().Foreground(brandPurple).Bold(true).Render(
		" Select sandbox mode",
	)
	sb.WriteString(header)
	sb.WriteString("\n\n")

	labels := commands.SandboxModeLabels()
	descs := commands.SandboxModeDescriptions()

	for i, label := range labels {
		prefix := "   "
		if i == m.sandboxCursor {
			prefix = lipgloss.NewStyle().Foreground(brandPurple).Render(" ❯ ")
		}
		displayLabel := label
		if i == m.sandboxCursor {
			displayLabel = lipgloss.NewStyle().Bold(true).Render(label)
		} else {
			displayLabel = lipgloss.NewStyle().Foreground(dimText).Render(label)
		}
		desc := lipgloss.NewStyle().Foreground(dimText).Render(" — " + descs[i])
		sb.WriteString(fmt.Sprintf("%s%d. %s%s\n", prefix, i+1, displayLabel, desc))
	}
	sb.WriteString("\n")
	return sb.String()
}
