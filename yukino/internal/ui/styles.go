package ui

import "github.com/charmbracelet/lipgloss"

var (
	accentColor = lipgloss.Color(Theme.Accent)
	dimText     = lipgloss.Color(Theme.Dim)
	mutedText   = lipgloss.Color(Theme.Muted)
	normalText  = lipgloss.Color(Theme.Text)
	brightText  = lipgloss.Color(Theme.Bright)
	greenText   = lipgloss.Color(Theme.Success)
	redText     = lipgloss.Color(Theme.Error)
	yellowText  = lipgloss.Color(Theme.Warning)
	errorStyle  = lipgloss.NewStyle().Foreground(redText).PaddingLeft(2)
	promptStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
)
