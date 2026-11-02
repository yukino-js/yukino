package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Ports of src/ui/selector-frame.tsx and src/ui/selector-layout.ts: the shared
// chrome used by every selector dialog.

type selectorChromeSpec struct {
	borders bool
	title   bool
	hint    bool
	detail  bool
	height  int
}

func selectorHintStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(dimText) }

// selectorChrome mirrors selectorChrome in src/ui/selector-layout.ts.
func selectorChrome(rows int, subtitle bool) selectorChromeSpec {
	spec := selectorChromeSpec{
		borders: rows >= 6,
		title:   rows >= 2,
		hint:    rows >= 3,
		detail:  subtitle && rows >= 8,
	}
	if spec.borders {
		spec.height += 2
	}
	if spec.title {
		spec.height++
	}
	if spec.hint {
		spec.height++
	}
	if spec.detail {
		spec.height++
	}
	return spec
}

// fitHint mirrors fitHint in src/ui/selector-frame.tsx.
func fitHint(hint string, width int) string {
	full := plainTerminalLine(hint)
	compact := strings.ReplaceAll(strings.ReplaceAll(full, "Escape", "Esc"), " navigate", "")
	var actions []string
	for _, part := range strings.Split(compact, " · ") {
		if strings.Contains(part, "Enter") || strings.Contains(part, "Esc") {
			actions = append(actions, part)
		}
	}
	keys := make([]string, 0, len(actions))
	for _, part := range actions {
		keys = append(keys, strings.SplitN(part, " ", 2)[0])
	}
	keysJoined := strings.Join(keys, " / ")
	actionsJoined := strings.Join(actions, " · ")
	for _, candidate := range []string{full, compact, actionsJoined, keysJoined} {
		if candidate != "" && visibleWidth(candidate) <= width {
			return truncateToWidth(candidate, width, "…")
		}
	}
	fallback := keysJoined
	if fallback == "" {
		fallback = compact
	}
	return truncateToWidth(fallback, width, "…")
}

type selectorFrameParams struct {
	title       string
	subtitle    string
	hint        string
	content     string
	width       int
	rows        int
	focusRow    int
	focusHeight int
}

// renderSelectorFrame mirrors SelectorFrame in src/ui/selector-frame.tsx.
func renderSelectorFrame(p selectorFrameParams) string {
	if p.rows <= 0 {
		return ""
	}
	selectorTitleStyle := lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	selectorSubtitleStyle := lipgloss.NewStyle().Foreground(mutedText)
	selectorBorderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(Theme.Border))
	chrome := selectorChrome(p.rows, p.subtitle != "")
	frameWidth := max(1, p.width)
	padding := 0
	if frameWidth > 2 {
		padding = 1
	}
	contentWidth := frameWidth - padding*2
	rule := strings.Repeat("─", max(0, frameWidth-2))
	top, bottom := "─", "─"
	if frameWidth >= 2 {
		top, bottom = "╭"+rule+"╮", "╰"+rule+"╯"
	}
	singleLine := func(text string) string {
		return truncateToWidth(plainTerminalLine(text), contentWidth, "…")
	}
	pad := func(line string) string {
		if padding == 0 {
			return line
		}
		return " " + line
	}

	var sb strings.Builder
	if chrome.borders {
		sb.WriteString(selectorBorderStyle.Render(top))
		sb.WriteString("\n")
	}
	if chrome.title {
		sb.WriteString(pad(selectorTitleStyle.Render(singleLine(p.title))))
		sb.WriteString("\n")
	}
	if chrome.detail && p.subtitle != "" {
		sb.WriteString(pad(selectorSubtitleStyle.Render(singleLine(p.subtitle))))
		sb.WriteString("\n")
	}
	lines := strings.Split(p.content, "\n")
	if p.content == "" {
		lines = nil
	}
	limit := max(0, p.rows-chrome.height)
	focusHeight := max(1, p.focusHeight)
	start := min(max(0, p.focusRow+min(focusHeight, limit)-limit), max(0, len(lines)-limit))
	for _, line := range lines[start:min(len(lines), start+limit)] {
		sb.WriteString(pad(truncateToWidth(line, contentWidth, "…")))
		sb.WriteString("\n")
	}
	if chrome.hint {
		sb.WriteString(pad(selectorHintStyle().Render(fitHint(p.hint, contentWidth))))
		sb.WriteString("\n")
	}
	if chrome.borders {
		sb.WriteString(selectorBorderStyle.Render(bottom))
		sb.WriteString("\n")
	}
	return sb.String()
}
