package ui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
)

func (m Model) renderMarkdown(content string) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithColorProfile(termenv.TrueColor),
		glamour.WithWordWrap(m.width-6),
	)
	if err != nil {
		return content
	}
	rendered, err := r.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSpace(rendered)
}
