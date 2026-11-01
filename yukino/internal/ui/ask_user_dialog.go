package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

func (m Model) handleAskUserDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	multiQuestion := len(m.askUserQuestions) > 1

	if m.askUserOnSubmit {
		switch msg.String() {
		case "up", "k":
			if m.askUserSubmitIdx > 0 {
				m.askUserSubmitIdx--
			}
			m.updateViewport()
			return m, nil
		case "down", "j":
			if m.askUserSubmitIdx < 1 {
				m.askUserSubmitIdx++
			}
			m.updateViewport()
			return m, nil
		case "left", "shift+tab":
			m.askUserOnSubmit = false
			m.askUserQIdx = len(m.askUserQuestions) - 1
			m.updateViewport()
			return m, nil
		case "enter":
			if m.askUserSubmitIdx == 0 {
				return m.submitAllAnswers()
			}
			return m.cancelAskUser()
		case "esc":
			return m.cancelAskUser()
		}
		return m, nil
	}

	q := m.askUserQuestions[m.askUserQIdx]
	optCount := len(q.Options) + 1
	cursor := m.askUserCursors[m.askUserQIdx]

	switch msg.String() {
	case "up", "k":
		if cursor > 0 {
			m.askUserCursors[m.askUserQIdx]--
		}
		m.updateViewport()
		return m, nil
	case "down", "j":
		if cursor < optCount-1 {
			m.askUserCursors[m.askUserQIdx]++
		}
		m.updateViewport()
		return m, nil
	case "left", "shift+tab":
		if multiQuestion && m.askUserQIdx > 0 {
			m.askUserQIdx--
			m.updateViewport()
		}
		return m, nil
	case "right", "tab":
		if multiQuestion {
			if m.askUserQIdx < len(m.askUserQuestions)-1 {
				m.askUserQIdx++
			} else {
				m.askUserOnSubmit = true
				m.askUserSubmitIdx = 0
			}
			m.updateViewport()
		}
		return m, nil
	case " ":
		if q.MultiSelect && cursor < len(q.Options) {
			sel := m.askUserSelected[m.askUserQIdx]
			sel[cursor] = !sel[cursor]
			m.updateViewport()
			return m, nil
		}
	case "enter":
		m.saveCurrentAnswer()

		if !multiQuestion && !q.MultiSelect {
			return m.submitAllAnswers()
		}

		if m.askUserQIdx < len(m.askUserQuestions)-1 {
			m.askUserQIdx++
		} else {
			m.askUserOnSubmit = true
			m.askUserSubmitIdx = 0
		}
		m.updateViewport()
		return m, nil
	case "backspace":
		if cursor == len(q.Options) && len(m.askUserOther[m.askUserQIdx]) > 0 {
			s := m.askUserOther[m.askUserQIdx]
			_, size := utf8.DecodeLastRuneInString(s)
			m.askUserOther[m.askUserQIdx] = s[:len(s)-size]
			m.updateViewport()
		}
		return m, nil
	case "esc":
		return m.cancelAskUser()
	default:
		if cursor == len(q.Options) && len(msg.Runes) > 0 {
			m.askUserOther[m.askUserQIdx] += string(msg.Runes)
			m.updateViewport()
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) saveCurrentAnswer() {
	q := m.askUserQuestions[m.askUserQIdx]
	cursor := m.askUserCursors[m.askUserQIdx]

	if cursor == len(q.Options) {
		other := m.askUserOther[m.askUserQIdx]
		if other == "" {
			other = "Other"
		}
		m.askUserAnswered[m.askUserQIdx] = other
	} else if q.MultiSelect {
		var selected []string
		for i, opt := range q.Options {
			if m.askUserSelected[m.askUserQIdx][i] {
				selected = append(selected, opt.Label)
			}
		}
		if len(selected) == 0 {
			selected = append(selected, q.Options[cursor].Label)
		}
		m.askUserAnswered[m.askUserQIdx] = strings.Join(selected, ", ")
	} else {
		m.askUserAnswered[m.askUserQIdx] = q.Options[cursor].Label
	}
}

func (m *Model) collectAskUserAnswers() map[string]string {
	result := make(map[string]string)
	for idx, answer := range m.askUserAnswered {
		if idx < len(m.askUserQuestions) {
			result[m.askUserQuestions[idx].Text] = answer
		}
	}
	return result
}

func (m *Model) submitAllAnswers() (tea.Model, tea.Cmd) {
	m.askUserDialog = false
	m.askUserRespCh <- tools.QuestionResponse{Answers: m.collectAskUserAnswers()}
	m.updateViewport()
	return m, tea.Batch(m.listenForAgentEvents(), m.listenForAskUser())
}

func (m *Model) cancelAskUser() (tea.Model, tea.Cmd) {
	m.askUserDialog = false
	m.askUserRespCh <- tools.QuestionResponse{Answers: map[string]string{"_declined": "true"}}
	m.updateViewport()
	return m, tea.Batch(m.listenForAgentEvents(), m.listenForAskUser())
}

func (m Model) renderAskUserDialog() string {
	if !m.askUserDialog || len(m.askUserQuestions) == 0 {
		return ""
	}
	var sb strings.Builder
	multiQuestion := len(m.askUserQuestions) > 1

	if multiQuestion {
		sb.WriteString(m.renderQuestionNavBar())
		sb.WriteString("\n\n")
	}

	if m.askUserOnSubmit {
		sb.WriteString(m.renderSubmitView())
	} else {
		sb.WriteString(m.renderQuestionView())
	}

	if multiQuestion && !m.askUserOnSubmit {
		hint := lipgloss.NewStyle().Foreground(dimText).Render("      ← → navigate questions · enter to confirm")
		sb.WriteString(hint)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (m Model) renderQuestionNavBar() string {
	var sb strings.Builder
	activeTab := lipgloss.NewStyle().Background(lipgloss.Color("99")).Foreground(lipgloss.Color("255")).Bold(true).Padding(0, 1)
	inactiveTab := lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Padding(0, 1)
	dimArrow := lipgloss.NewStyle().Foreground(dimText)
	brightArrow := lipgloss.NewStyle().Foreground(brandPurple).Bold(true)

	if m.askUserQIdx == 0 && !m.askUserOnSubmit {
		sb.WriteString(dimArrow.Render(" ←"))
	} else {
		sb.WriteString(brightArrow.Render(" ←"))
	}

	for i, q := range m.askUserQuestions {
		header := q.Header
		if header == "" {
			header = fmt.Sprintf("Q%d", i+1)
		}
		_, answered := m.askUserAnswered[i]
		check := "☐"
		if answered {
			check = "☑"
		}
		label := header + " " + check

		if !m.askUserOnSubmit && i == m.askUserQIdx {
			sb.WriteString(activeTab.Render(label))
		} else {
			sb.WriteString(inactiveTab.Render(label))
		}
	}

	submitLabel := "✓ Submit"
	if m.askUserOnSubmit {
		sb.WriteString(activeTab.Render(submitLabel))
	} else {
		sb.WriteString(inactiveTab.Render(submitLabel))
	}

	if m.askUserOnSubmit {
		sb.WriteString(dimArrow.Render(" →"))
	} else {
		sb.WriteString(brightArrow.Render(" →"))
	}

	return sb.String()
}

func (m Model) askUserMaxLines() int {
	maxLines := 0
	for _, q := range m.askUserQuestions {
		lines := 2 + len(q.Options) + 1
		if q.MultiSelect {
			lines++
		}
		if lines > maxLines {
			maxLines = lines
		}
	}
	return maxLines
}

func (m Model) renderQuestionView() string {
	var sb strings.Builder
	q := m.askUserQuestions[m.askUserQIdx]
	cursor := m.askUserCursors[m.askUserQIdx]
	lines := 0

	header := lipgloss.NewStyle().Foreground(brandPurple).Bold(true).Render(" " + q.Text)
	sb.WriteString(header)
	sb.WriteString("\n\n")
	lines += 2

	for i, opt := range q.Options {
		prefix := "   "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(brandPurple).Render(" ❯ ")
		}
		if q.MultiSelect {
			check := "○"
			if m.askUserSelected[m.askUserQIdx][i] {
				check = "●"
			}
			prefix += check + " "
		}
		label := opt.Label
		if i == cursor {
			label = lipgloss.NewStyle().Bold(true).Render(opt.Label)
		}
		desc := lipgloss.NewStyle().Foreground(dimText).Render(" — " + opt.Description)
		sb.WriteString(fmt.Sprintf("%s%s%s\n", prefix, label, desc))
		lines++
	}

	otherIdx := len(q.Options)
	prefix := "   "
	if cursor == otherIdx {
		prefix = lipgloss.NewStyle().Foreground(brandPurple).Render(" ❯ ")
	}
	otherLabel := "Other"
	if cursor == otherIdx {
		otherLabel = lipgloss.NewStyle().Bold(true).Render("Other")
	} else {
		otherLabel = lipgloss.NewStyle().Foreground(dimText).Render("Other")
	}
	sb.WriteString(fmt.Sprintf("%s%s", prefix, otherLabel))
	if cursor == otherIdx {
		input := m.askUserOther[m.askUserQIdx] + "█"
		sb.WriteString(": " + input)
	}
	sb.WriteString("\n")
	lines++

	if q.MultiSelect {
		sb.WriteString(lipgloss.NewStyle().Foreground(dimText).Render("      space to toggle, enter to confirm"))
		sb.WriteString("\n")
		lines++
	}

	if len(m.askUserQuestions) > 1 {
		target := m.askUserMaxLines()
		for lines < target {
			sb.WriteString("\n")
			lines++
		}
	}

	return sb.String()
}

func (m Model) renderSubmitView() string {
	var sb strings.Builder
	lines := 0

	header := lipgloss.NewStyle().Foreground(brandPurple).Bold(true).Render(" Review your answers:")
	sb.WriteString(header)
	sb.WriteString("\n\n")
	lines += 2

	for i, q := range m.askUserQuestions {
		label := q.Header
		if label == "" {
			label = fmt.Sprintf("Q%d", i+1)
		}
		answer, ok := m.askUserAnswered[i]
		if ok {
			sb.WriteString(fmt.Sprintf("   %s: %s\n", label, answer))
		} else {
			dim := lipgloss.NewStyle().Foreground(dimText).Render(fmt.Sprintf("   %s: (not answered)", label))
			sb.WriteString(dim + "\n")
		}
		lines++
	}
	sb.WriteString("\n")
	lines++

	for i, opt := range []string{"Submit answers", "Cancel"} {
		if i == m.askUserSubmitIdx {
			prefix := lipgloss.NewStyle().Foreground(brandPurple).Render(" ❯ ")
			label := lipgloss.NewStyle().Bold(true).Render(opt)
			sb.WriteString(prefix + label + "\n")
		} else {
			sb.WriteString("   " + opt + "\n")
		}
		lines++
	}

	target := m.askUserMaxLines()
	for lines < target {
		sb.WriteString("\n")
		lines++
	}

	return sb.String()
}
