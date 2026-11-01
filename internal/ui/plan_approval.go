package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/plan_file"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/prompt"
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
		if m.planApprovalCursor == 2 && m.planApprovalInput != "" {
			return m.sendPlanFeedback(m.planApprovalInput, false)
		}
		return m.executePlanApproval()
	case "shift+tab":
		if m.planApprovalCursor == 2 && m.planApprovalInput != "" {
			return m.sendPlanFeedback(m.planApprovalInput, true)
		}
		return m, nil
	case "esc":
		m.planApprovalDialog = false
		m.updateViewport()
		return m, nil
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

	var modeMsg string
	switch m.planApprovalCursor {
	case 0:
		if m.ag != nil && m.ag.Checker != nil {
			m.ag.Checker.Mode = permissions.ModeBypass
			m.ag.Checker.PlanFilePath = ""
		}
		modeMsg = "Plan approved. Entered YOLO mode (all operations auto-approved)."
	case 1:
		if m.ag != nil && m.ag.Checker != nil {
			restoreMode := m.prePlanMode
			if restoreMode == "" {
				restoreMode = permissions.ModeDefault
			}
			m.ag.Checker.Mode = restoreMode
			m.ag.Checker.PlanFilePath = ""
		}
		modeMsg = "Plan approved. Each edit will require your confirmation."
	}

	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "system",
		content: modeMsg,
	})

	planPath := plan_file.GetPlanFilePath()
	planContent, _ := plan_file.LoadPlan()
	planExists := plan_file.PlanExists()
	plan_file.ResetPlanPath()

	executeMsg := prompt.BuildPlanModeExitReminder(planPath, planExists)
	m.hasExitedPlanMode = true
	executeMsg += "\n\nUser has approved your plan. You can now start coding."
	if planContent != "" {
		executeMsg += "\n\nApproved Plan:\n" + planContent
	}

	m.updateViewport()
	return m.sendMessage(executeMsg)
}

func (m Model) sendPlanFeedback(feedback string, alsoExit bool) (tea.Model, tea.Cmd) {
	m.planApprovalDialog = false
	m.planApprovalInput = ""

	if alsoExit {
		if m.ag != nil && m.ag.Checker != nil {
			restoreMode := m.prePlanMode
			if restoreMode == "" {
				restoreMode = permissions.ModeDefault
			}
			m.ag.Checker.Mode = restoreMode
			m.ag.Checker.PlanFilePath = ""
		}
		plan_file.ResetPlanPath()
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "system",
			content: "Exiting plan mode with feedback. Edits will require confirmation.",
		})
	}

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
			hint := lipgloss.NewStyle().Foreground(dimText).Render("      shift+tab to approve with this feedback")
			sb.WriteString(hint)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
	return sb.String()
}
