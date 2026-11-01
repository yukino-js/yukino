package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/mcp"
)

var loginFieldKeys = []string{"name", "protocol", "base_url", "api_key", "model", "thinking", "context_window", "max_output_tokens"}

var loginFieldLabels = map[string]string{
	"name":              "Name",
	"protocol":          "Protocol",
	"base_url":          "Base URL",
	"api_key":           "API key",
	"model":             "Model",
	"thinking":          "Thinking",
	"context_window":    "Context window",
	"max_output_tokens": "Max output tokens",
}

type providerLoginForm struct {
	name            string
	protocol        string
	baseURL         string
	apiKey          string
	model           string
	thinking        string
	contextWindow   string
	maxOutputTokens string
}

func newProviderLoginForm() providerLoginForm {
	return providerLoginForm{
		protocol:        "anthropic",
		thinking:        config.DefaultThinkingLevel,
		contextWindow:   strconv.Itoa(config.DefaultContextWindow),
		maxOutputTokens: strconv.Itoa(config.DefaultMaxOutputTokens),
	}
}

func (f *providerLoginForm) field(field string) *string {
	switch field {
	case "name":
		return &f.name
	case "protocol":
		return &f.protocol
	case "base_url":
		return &f.baseURL
	case "api_key":
		return &f.apiKey
	case "model":
		return &f.model
	case "thinking":
		return &f.thinking
	case "context_window":
		return &f.contextWindow
	case "max_output_tokens":
		return &f.maxOutputTokens
	}
	return nil
}

func (f *providerLoginForm) toInput() map[string]any {
	input := map[string]any{
		"name":     f.name,
		"protocol": f.protocol,
		"base_url": f.baseURL,
		"api_key":  f.apiKey,
		"model":    f.model,
		"thinking": f.thinking,
	}
	if cw, err := strconv.Atoi(strings.TrimSpace(f.contextWindow)); err == nil {
		input["context_window"] = cw
	}
	if mot, err := strconv.Atoi(strings.TrimSpace(f.maxOutputTokens)); err == nil {
		input["max_output_tokens"] = mot
	}
	return input
}

func (m Model) openLoginDialog() (tea.Model, tea.Cmd) {
	m.loginDialog = true
	m.loginForm = newProviderLoginForm()
	m.loginCursor = 0
	m.loginError = ""
	m.updateViewport()
	return m, nil
}

func (m Model) closeLoginDialog() (tea.Model, tea.Cmd) {
	m.loginDialog = false
	m.updateViewport()
	return m, nil
}

func (m Model) handleLoginDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	field := loginFieldKeys[m.loginCursor]
	switch msg.String() {
	case "esc":
		return m.closeLoginDialog()
	case "up":
		m.loginCursor = (m.loginCursor - 1 + len(loginFieldKeys)) % len(loginFieldKeys)
		m.loginError = ""
		return m, nil
	case "down":
		m.loginCursor = (m.loginCursor + 1) % len(loginFieldKeys)
		m.loginError = ""
		return m, nil
	case "left", "right":
		if field == "protocol" {
			protocols := []string{"anthropic", "openai", "openai-compat"}
			idx := 0
			for i, p := range protocols {
				if p == m.loginForm.protocol {
					idx = i
				}
			}
			if msg.String() == "right" {
				idx = (idx + 1) % len(protocols)
			} else {
				idx = (idx - 1 + len(protocols)) % len(protocols)
			}
			m.loginForm.protocol = protocols[idx]
		} else if field == "thinking" {
			idx := 0
			for i, l := range config.ThinkingLevels {
				if l == m.loginForm.thinking {
					idx = i
				}
			}
			if msg.String() == "right" {
				idx = (idx + 1) % len(config.ThinkingLevels)
			} else {
				idx = (idx - 1 + len(config.ThinkingLevels)) % len(config.ThinkingLevels)
			}
			m.loginForm.thinking = config.ThinkingLevels[idx]
		}
		return m, nil
	case "enter":
		return m.submitLogin()
	case "backspace":
		if target := m.loginForm.field(field); target != nil && len(*target) > 0 {
			*target = (*target)[:len(*target)-1]
		}
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			if target := m.loginForm.field(field); target != nil {
				*target += string(msg.Runes)
			}
		}
		return m, nil
	}
}

func (m Model) submitLogin() (tea.Model, tea.Cmd) {
	input := m.loginForm.toInput()
	provider, err := config.ParseProviderLogin(input)
	if err != nil {
		m.loginError = err.Error()
		return m, nil
	}
	result, err := config.SaveProvider(input, m.providers)
	if err != nil {
		m.loginError = err.Error()
		return m, nil
	}
	_ = result
	m.loginDialog = false

	// Activate the saved provider.
	m.providers = append(m.providers[:0], result.Providers...)
	selected := &m.providers[0]
	for i := range m.providers {
		if m.providers[i].BaseURL == provider.BaseURL {
			selected = &m.providers[i]
			break
		}
	}
	m.selectedProvider = selected
	wd := m.getWorkDir()
	client, err := llm.NewClient(selected, m.loadSkillsAndBuildPrompt(wd))
	if err != nil {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "error",
			content: fmt.Sprintf("Provider saved but client creation failed: %s", err),
		})
		m.updateViewport()
		return m, nil
	}
	m.client = client
	if m.ag != nil {
		m.ag.Client = client
		m.ag.ContextWindow = config.GetContextWindow(selected)
		m.ag.MaxOutputTokens = config.GetMaxOutputTokens(selected)
	}
	mcp.DecideAndApply(m.registry, selected.BaseURL, config.GetContextWindow(selected))
	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "system",
		content: fmt.Sprintf("Provider %q configured and activated.", provider.Name),
	})
	m.updateViewport()
	return m, nil
}

func (m Model) renderLoginDialog() string {
	if !m.loginDialog {
		return ""
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(Theme.BorderAccent)).
		Padding(1, 2)

	var inner strings.Builder
	inner.WriteString(selectLabelStyle.Render("Configure provider"))
	inner.WriteString("\n\n")

	for i, field := range loginFieldKeys {
		label := loginFieldLabels[field]
		value := ""
		if target := m.loginForm.field(field); target != nil {
			value = *target
		}
		if field == "api_key" && value != "" {
			value = strings.Repeat("•", len(value))
		}
		line := fmt.Sprintf("%-16s %s", label+":", value)
		if i == m.loginCursor {
			line += "█"
			inner.WriteString(selectedItemStyle.Render("❯ " + line))
		} else {
			inner.WriteString(normalItemStyle.Render("  " + line))
		}
		inner.WriteString("\n")
	}
	inner.WriteString("\n")
	if m.loginError != "" {
		inner.WriteString(errorStyle.Render(m.loginError))
		inner.WriteString("\n")
	}
	inner.WriteString(permDimStyle.Render("↑↓ field · ←→ cycle protocol/thinking · Enter save · Esc cancel"))
	return border.Render(inner.String()) + "\n"
}
