package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/plan_file"
	"github.com/yukino-js/yukino/yukino/internal/prompt"
)

func (m Model) handlePlanApproval(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.planApprovalCursor > 0 {
			m.planApprovalCursor--
		}
		m.updateViewport()
		return m, nil
	case "down", "j":
		if m.planApprovalCursor < 2 {
			m.planApprovalCursor++
		}
		m.updateViewport()
		return m, nil
	case "enter":
		if m.planApprovalCursor == 2 {
			return m.sendPlanFeedback(m.planApprovalInput)
		}
		return m.executePlanApproval()
	case "shift+tab":
		if m.planApprovalCursor == 2 {
			return m.sendPlanFeedback(m.planApprovalInput)
		}
		return m, nil
	case "esc":
		m.planApprovalCursor = 1
		return m.executePlanApproval()
	case "backspace":
		if m.planApprovalCursor == 2 && len(m.planApprovalInput) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.planApprovalInput)
			m.planApprovalInput = m.planApprovalInput[:len(m.planApprovalInput)-size]
			m.updateViewport()
		}
		return m, nil
	default:
		if m.planApprovalCursor == 2 && len(msg.Runes) > 0 {
			m.planApprovalInput += string(msg.Runes)
			m.updateViewport()
		}
		return m, nil
	}
}

func (m Model) executePlanApproval() (tea.Model, tea.Cmd) {
	m.planApprovalDialog = false
	if m.ag == nil || m.ag.Checker == nil {
		m.updateViewport()
		return m, nil
	}
	checker := m.ag.Checker
	planPath := plan_file.GetCurrentPlanPath(checker)
	planContent, _ := plan_file.LoadPlan(checker)
	planExists := plan_file.PlanExists(checker)
	mode, message := permissions.ModeDefault, "Plan approved. Edits will require your confirmation."
	if m.planApprovalCursor == 0 {
		mode, message = permissions.ModeBypass, "Plan approved. Entered YOLO mode (all operations auto-approved)."
	}
	checker.ExitPlanMode(mode)
	m.conversation.AddSystemReminder(prompt.BuildPlanModeExitReminder(planPath, planExists))
	m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: message})
	m.updateViewport()
	if strings.TrimSpace(planContent) == "" {
		return m, nil
	}
	return m.sendMessage("Execute this plan:\n\n" + planContent)
}

func (m Model) sendPlanFeedback(feedback string) (tea.Model, tea.Cmd) {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		m.planFeedbackRequired = true
		m.updateViewport()
		return m, nil
	}
	m.planApprovalDialog = false
	m.planApprovalInput = ""
	m.planFeedbackRequired = false
	m.updateViewport()
	return m.sendMessage(feedback)
}

func (m Model) renderPlanApprovalDialog() string {
	var sb strings.Builder

	header := lipgloss.NewStyle().Foreground(brandPurple).Bold(true).Render(
		" Yukino has written up a plan and is ready to execute. Would you like to proceed?",
	)
	sb.WriteString(header)
	sb.WriteString("\n\n")

	options := []string{
		"Yes, enter YOLO mode (auto-approve all)",
		"Yes, manually approve edits",
		"Tell Yukino what to change",
	}

	for i, opt := range options {
		prefix := "   "
		if i == m.planApprovalCursor {
			prefix = lipgloss.NewStyle().Foreground(brandPurple).Render(" ❯ ")
		}
		label := opt
		if i == m.planApprovalCursor {
			label = lipgloss.NewStyle().Bold(true).Render(opt)
		} else {
			label = lipgloss.NewStyle().Foreground(dimText).Render(opt)
		}
		sb.WriteString(prefix)
		sb.WriteString(fmt.Sprintf("%d. %s", i+1, label))
		sb.WriteString("\n")

		if i == 2 {
			inputLine := m.planApprovalInput
			if m.planApprovalCursor == 2 {
				inputLine += "█"
			}
			if inputLine == "█" || inputLine == "" {
				placeholder := lipgloss.NewStyle().Foreground(dimText).Render("Type feedback here...")
				if m.planApprovalCursor == 2 {
					sb.WriteString("      " + placeholder + "\n")
				}
			} else {
				sb.WriteString("      " + inputLine + "\n")
			}
			hint := lipgloss.NewStyle().Foreground(dimText).Render("      shift+tab to submit feedback")
			sb.WriteString(hint)
			sb.WriteString("\n")
		}
	}
	if m.planFeedbackRequired {
		sb.WriteString("      Enter feedback before submitting.\n")
	}
	sb.WriteString("\n")
	return sb.String()
}
