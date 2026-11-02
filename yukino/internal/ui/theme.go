package ui

// ThemePalette mirrors src/theme/constants.ts. Values match
// src/theme/palettes.js (Catppuccin Mocha for dark, Latte for light).
type ThemePalette struct {
	Accent             string
	Border             string
	BorderAccent       string
	BorderMuted        string
	Success            string
	Error              string
	Warning            string
	Muted              string
	Dim                string
	Text               string
	ThinkingText       string
	SelectedBg         string
	UserMessageBg      string
	UserMessageText    string
	CustomMessageBg    string
	CustomMessageText  string
	CustomMessageLabel string
	ToolPendingBg      string
	ToolSuccessBg      string
	ToolErrorBg        string
	ToolTitle          string
	ToolOutput         string
	MdHeading          string
	MdLink             string
	MdLinkURL          string
	MdCode             string
	MdCodeBlock        string
	MdCodeBlockBorder  string
	MdQuote            string
	MdQuoteBorder      string
	MdHr               string
	MdListBullet       string
	ToolDiffAdded      string
	ToolDiffRemoved    string
	ToolDiffContext    string
	SyntaxComment      string
	SyntaxKeyword      string
	SyntaxFunction     string
	SyntaxVariable     string
	SyntaxString       string
	SyntaxNumber       string
	SyntaxType         string
	SyntaxOperator     string
	SyntaxPunctuation  string
	ThinkingOff        string
	ThinkingMinimal    string
	ThinkingLow        string
	ThinkingMedium     string
	ThinkingHigh       string
	ThinkingMax        string
	BashMode           string
	ThinkingXHigh      string
	Background         string
	Surface            string
	CodeBg             string
	Bright             string
	AccentContrast     string
}

// ThemeMode mirrors src/theme/constants.ts ThemeMode.
type ThemeMode string

const (
	ThemeModeDark  ThemeMode = "dark"
	ThemeModeLight ThemeMode = "light"
)

// DarkTheme is Catppuccin Mocha, matching palettes.js dark.
var DarkTheme = ThemePalette{
	Accent:             "#94e2d5",
	Border:             "#45475a",
	BorderAccent:       "#94e2d5",
	BorderMuted:        "#313244",
	Success:            "#a6e3a1",
	Error:              "#f38ba8",
	Warning:            "#f9e2af",
	Muted:              "#bac2de",
	Dim:                "#a6adc8",
	Text:               "#cdd6f4",
	ThinkingText:       "#a6adc8",
	SelectedBg:         "#45475a",
	UserMessageBg:      "#313244",
	UserMessageText:    "#cdd6f4",
	CustomMessageBg:    "#313244",
	CustomMessageText:  "#cdd6f4",
	CustomMessageLabel: "#cba6f7",
	ToolPendingBg:      "#181825",
	ToolSuccessBg:      "#181825",
	ToolErrorBg:        "#181825",
	ToolTitle:          "#cdd6f4",
	ToolOutput:         "#bac2de",
	MdHeading:          "#cba6f7",
	MdLink:             "#89b4fa",
	MdLinkURL:          "#a6adc8",
	MdCode:             "#94e2d5",
	MdCodeBlock:        "#cdd6f4",
	MdCodeBlockBorder:  "#585b70",
	MdQuote:            "#a6adc8",
	MdQuoteBorder:      "#585b70",
	MdHr:               "#585b70",
	MdListBullet:       "#94e2d5",
	ToolDiffAdded:      "#a6e3a1",
	ToolDiffRemoved:    "#f38ba8",
	ToolDiffContext:    "#a6adc8",
	SyntaxComment:      "#9399b2",
	SyntaxKeyword:      "#cba6f7",
	SyntaxFunction:     "#89b4fa",
	SyntaxVariable:     "#cdd6f4",
	SyntaxString:       "#a6e3a1",
	SyntaxNumber:       "#fab387",
	SyntaxType:         "#f9e2af",
	SyntaxOperator:     "#89dceb",
	SyntaxPunctuation:  "#9399b2",
	ThinkingOff:        "#a6adc8",
	ThinkingMinimal:    "#bac2de",
	ThinkingLow:        "#89b4fa",
	ThinkingMedium:     "#94e2d5",
	ThinkingHigh:       "#cba6f7",
	ThinkingMax:        "#f5c2e7",
	BashMode:           "#a6e3a1",
	ThinkingXHigh:      "#b4befe",
	Background:         "#1e1e2e",
	Surface:            "#181825",
	CodeBg:             "#181825",
	Bright:             "#cdd6f4",
	AccentContrast:     "#1e1e2e",
}

// LightTheme is Catppuccin Latte, matching palettes.js light.
var LightTheme = ThemePalette{
	Accent:             "#179299",
	Border:             "#bcc0cc",
	BorderAccent:       "#179299",
	BorderMuted:        "#ccd0da",
	Success:            "#40a02b",
	Error:              "#d20f39",
	Warning:            "#df8e1d",
	Muted:              "#5c5f77",
	Dim:                "#6c6f85",
	Text:               "#4c4f69",
	ThinkingText:       "#6c6f85",
	SelectedBg:         "#bcc0cc",
	UserMessageBg:      "#ccd0da",
	UserMessageText:    "#4c4f69",
	CustomMessageBg:    "#ccd0da",
	CustomMessageText:  "#4c4f69",
	CustomMessageLabel: "#8839ef",
	ToolPendingBg:      "#e6e9ef",
	ToolSuccessBg:      "#e6e9ef",
	ToolErrorBg:        "#e6e9ef",
	ToolTitle:          "#4c4f69",
	ToolOutput:         "#5c5f77",
	MdHeading:          "#8839ef",
	MdLink:             "#1e66f5",
	MdLinkURL:          "#6c6f85",
	MdCode:             "#179299",
	MdCodeBlock:        "#4c4f69",
	MdCodeBlockBorder:  "#acb0be",
	MdQuote:            "#6c6f85",
	MdQuoteBorder:      "#acb0be",
	MdHr:               "#acb0be",
	MdListBullet:       "#179299",
	ToolDiffAdded:      "#40a02b",
	ToolDiffRemoved:    "#d20f39",
	ToolDiffContext:    "#6c6f85",
	SyntaxComment:      "#7c7f93",
	SyntaxKeyword:      "#8839ef",
	SyntaxFunction:     "#1e66f5",
	SyntaxVariable:     "#4c4f69",
	SyntaxString:       "#40a02b",
	SyntaxNumber:       "#fe640b",
	SyntaxType:         "#df8e1d",
	SyntaxOperator:     "#04a5e5",
	SyntaxPunctuation:  "#7c7f93",
	ThinkingOff:        "#6c6f85",
	ThinkingMinimal:    "#5c5f77",
	ThinkingLow:        "#1e66f5",
	ThinkingMedium:     "#179299",
	ThinkingHigh:       "#8839ef",
	ThinkingMax:        "#ea76cb",
	BashMode:           "#40a02b",
	ThinkingXHigh:      "#7287fd",
	Background:         "#eff1f5",
	Surface:            "#e6e9ef",
	CodeBg:             "#e6e9ef",
	Bright:             "#4c4f69",
	AccentContrast:     "#eff1f5",
}

// Palettes mirrors src/theme/constants.ts PALETTES.
var Palettes = struct {
	Dark  ThemePalette
	Light ThemePalette
}{Dark: DarkTheme, Light: LightTheme}

// PaletteFor mirrors the reference palette selection by mode.
func PaletteFor(mode ThemeMode) ThemePalette {
	if mode == ThemeModeLight {
		return LightTheme
	}
	return DarkTheme
}

// Theme is the active palette; the terminal UI defaults to the dark theme.
var Theme = DarkTheme
