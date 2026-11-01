package ui

import "github.com/charmbracelet/lipgloss"

// Palette colors derived from the active theme (src/theme/palettes.js).
var (
	accentColor = lipgloss.Color(Theme.Accent)
	dimText     = lipgloss.Color(Theme.Dim)
	mutedText   = lipgloss.Color(Theme.Muted)
	normalText  = lipgloss.Color(Theme.Text)
	brightText  = lipgloss.Color(Theme.Bright)
	greenText   = lipgloss.Color(Theme.Success)
	redText     = lipgloss.Color(Theme.Error)
	yellowText  = lipgloss.Color(Theme.Warning)
	cyanText    = lipgloss.Color(Theme.BorderAccent)

	// brandPurple is the historical name kept for the primary accent.
	brandPurple = accentColor
)

var (
	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Theme.BorderMuted))

	promptStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	aiMarkerStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	aiTextStyle = lipgloss.NewStyle().
			Foreground(normalText).
			PaddingLeft(2)

	streamingTextStyle = lipgloss.NewStyle().
				Foreground(normalText).
				PaddingLeft(2)

	toolRunningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(Theme.ToolOutput)).
				PaddingLeft(2)

	toolDoneStyle = lipgloss.NewStyle().
			Foreground(greenText).
			PaddingLeft(2)

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(redText).
			PaddingLeft(2)

	toolDetailStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Theme.ToolOutput)).
			PaddingLeft(4)

	diffAddStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Theme.ToolDiffAdded)).
			PaddingLeft(4)

	diffRemoveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(Theme.ToolDiffRemoved)).
			PaddingLeft(4)

	errorStyle = lipgloss.NewStyle().
			Foreground(redText).
			PaddingLeft(2)

	permBorderStyle = lipgloss.NewStyle().
			Foreground(yellowText).
			Bold(true)

	permDimStyle = lipgloss.NewStyle().
			Foreground(dimText)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(dimText)

	statusItemStyle = lipgloss.NewStyle().
			Foreground(mutedText)

	selectLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor).
				Align(lipgloss.Center)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true)

	normalItemStyle = lipgloss.NewStyle().
			Foreground(mutedText)

	menuActiveStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)
)
