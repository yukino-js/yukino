package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/llm"
)

func thinkingLevelColor(level string) lipgloss.Color {
	switch level {
	case "off":
		return lipgloss.Color(Theme.ThinkingOff)
	case "minimal":
		return lipgloss.Color(Theme.ThinkingMinimal)
	case "low":
		return lipgloss.Color(Theme.ThinkingLow)
	case "medium":
		return lipgloss.Color(Theme.ThinkingMedium)
	case "high":
		return lipgloss.Color(Theme.ThinkingHigh)
	case "xhigh":
		return lipgloss.Color(Theme.ThinkingXHigh)
	case "max":
		return lipgloss.Color(Theme.ThinkingMax)
	}
	return lipgloss.Color(Theme.ThinkingHigh)
}
func (m Model) thinkingLevel() string {
	if client, ok := m.client.(llm.ThinkingLevelControl); ok {
		return client.GetThinkingLevel()
	}
	if m.selectedProvider == nil {
		return config.DefaultThinkingLevel
	}
	return config.GetThinkingLevel(m.selectedProvider)
}

func statusBorder(width int, status, spinner string, hidden int, down bool) string {
	columns := max(0, width)
	overflow := ""
	if hidden > 0 {
		direction := "↑"
		if down {
			direction = "↓"
		}
		overflow = fmt.Sprintf(" %s %d more ", direction, hidden)
	}
	overflowWidth := visibleWidth(overflow)
	overflowStart := (columns - overflowWidth) / 2
	label := ""
	if status != "" {
		label = truncateToWidth(plainTerminalLine(spinner+" "+status), max(0, columns-5), "…")
		if columns < 6 {
			label = spinner
		}
	}
	canFit := func() bool {
		return overflowWidth > 0 && overflowWidth+2 <= columns && (label == "" || overflowStart-(4+visibleWidth(label)) >= 1)
	}
	if label != "" && overflow != "" && !canFit() {
		label = spinner
	}
	if canFit() {
		left := ""
		if label != "" {
			left = "── " + label + " "
		}
		return left + strings.Repeat("─", max(0, overflowStart-visibleWidth(left))) + overflow + strings.Repeat("─", max(0, columns-overflowStart-overflowWidth))
	}
	if label != "" && columns >= visibleWidth(label)+5 {
		return "── " + label + " " + strings.Repeat("─", columns-visibleWidth(label)-4)
	}
	if label != "" {
		compact := truncateToWidth(spinner, columns, "")
		prefix := min(3, max(0, columns-visibleWidth(compact)))
		return strings.Repeat("─", prefix) + compact + strings.Repeat("─", max(0, columns-prefix-visibleWidth(compact)))
	}
	return strings.Repeat("─", columns)
}

func (m Model) completionRows(available int) int {
	borderRows := min(2, max(0, available-1))
	maximum := max(1, min(max(5, m.height*3/10), available-borderRows))
	width := max(1, m.width-boolColumn(m.width > 2)*2)
	rows := layoutInputRows(m.editor.lines, width, m.editor.pastes)
	return max(0, min(8, available-borderRows-min(len(rows), maximum)-1-boolColumn(m.pasteError != "" || m.pastingImage)))
}

func (m Model) renderComposer(available int) string {
	width := max(1, m.width)
	padding := boolColumn(width > 2)
	contentWidth := width - padding*2
	borderRows := min(2, max(0, available-1))
	maximum := max(1, min(max(5, m.height*3/10), available-borderRows))
	rows := layoutInputRows(m.editor.lines, contentWidth, m.editor.pastes)
	cursor := locateInputCursor(rows, m.editor.line, m.editor.col)
	start := max(0, min(cursor.row-maximum/2, len(rows)-maximum))
	end := min(len(rows), start+maximum)
	completionRows := m.completionRows(available)
	color := thinkingLevelColor(m.thinkingLevel())
	status := ""
	spinnerText := SpinnerFrames[0]
	if m.streaming {
		spinnerText = strings.TrimSpace(plainTerminalText(m.spinner.View()))
		if spinnerText == "" {
			spinnerText = SpinnerFrames[0]
		}
		status = "Working"
	}
	if m.retryStatus != "" {
		status = m.retryStatus
		color = yellowText
	}
	if m.compacting {
		status = "Compacting context... (Esc to cancel)"
		color = accentColor
	}
	if m.activityError != "" {
		status = "Error"
		color = redText
		spinnerText = "!"
	}
	if m.activityError == "" && strings.HasPrefix(m.editor.Value(), "!") {
		color = lipgloss.Color(Theme.BashMode)
	}
	borderStyle := lipgloss.NewStyle().Foreground(color)
	lines := []string{}
	if borderRows == 2 {
		lines = append(lines, borderStyle.Render(statusBorder(width, status, spinnerText, start, false)))
	}
	for i := start; i < end; i++ {
		row := rows[i]
		var line strings.Builder
		for j, cell := range row.cells {
			style := lipgloss.NewStyle().Foreground(normalText)
			if i == cursor.row && j == cursor.cell {
				style = style.Reverse(true)
			}
			line.WriteString(style.Render(cell.text))
		}
		if i == cursor.row && m.editor.line == 0 && m.editor.col == len([]rune(m.editor.lines[0])) && len(m.editor.lines) == 1 && strings.HasPrefix(m.editor.Value(), "/") && len(m.editor.Value()) > 1 && len(m.slashMatches) > 0 {
			typed := strings.ToLower(strings.TrimPrefix(m.editor.Value(), "/"))
			best := m.slashMatches[0].Name
			if strings.HasPrefix(strings.ToLower(best), typed) {
				ghost := string([]rune(best)[len([]rune(typed)):])
				line.WriteString(lipgloss.NewStyle().Foreground(dimText).Render(truncateToWidth(ghost, max(0, contentWidth-row.width), "…")))
			}
		}
		lines = append(lines, strings.Repeat(" ", padding)+line.String())
	}
	if borderRows > 0 {
		lines = append(lines, borderStyle.Render(statusBorder(width, "", spinnerText, len(rows)-end, true)))
	}
	if m.pastingImage {
		lines = append(lines, strings.Repeat(" ", padding)+lipgloss.NewStyle().Foreground(mutedText).Render(truncateToWidth("Reading clipboard image…", contentWidth, "…")))
	}
	if m.pasteError != "" {
		lines = append(lines, strings.Repeat(" ", padding)+lipgloss.NewStyle().Foreground(redText).Render(truncateToWidth("Error: "+m.pasteError, contentWidth, "…")))
	}
	if completionRows > 0 {
		if m.slashMenuOpen {
			lines = append(lines, m.renderSlashMenu(completionRows))
		} else if m.atMenuOpen {
			lines = append(lines, m.renderAtMenu(completionRows))
		}
	}
	return strings.Join(lines, "\n")
}
