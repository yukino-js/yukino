package ui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/mcp"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/subagent"
)

type ModelPickerStatus string

const (
	ModelPickerLoading ModelPickerStatus = "loading"
	ModelPickerReady   ModelPickerStatus = "ready"
	ModelPickerEmpty   ModelPickerStatus = "empty"
	ModelPickerError   ModelPickerStatus = "error"
)

type modelPickerState struct {
	status ModelPickerStatus
	models []llm.DiscoveredModel
}

type modelDiscoveryDoneMsg struct {
	models []llm.DiscoveredModel
	err    error
}

func (m Model) startModelDiscovery() tea.Cmd {
	if m.selectedProvider == nil {
		return nil
	}
	provider := *m.selectedProvider
	return func() tea.Msg {
		models, err := llm.DiscoverModels(context.Background(), llm.DiscoveryConfig{
			Protocol: provider.Protocol,
			BaseURL:  provider.BaseURL,
			APIKey:   config.ResolveAPIKey(&provider),
		})
		return modelDiscoveryDoneMsg{models: models, err: err}
	}
}

func (m Model) openModelDialog() (tea.Model, tea.Cmd) {
	if m.selectedProvider == nil {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "error",
			content: "Model switching requires an active provider.",
		})
		m.updateViewport()
		return m, nil
	}
	m.modelDialog = true
	m.modelState = modelPickerState{status: ModelPickerLoading}
	m.modelCursor = 0
	m.modelQuery = ""
	m.modelCustomMode = false
	m.modelCustomText = ""
	m.updateViewport()
	return m, m.startModelDiscovery()
}

func (m Model) closeModelDialog() (tea.Model, tea.Cmd) {
	m.modelDialog = false
	m.updateViewport()
	return m, nil
}

// modelEntries lists the visible model ids followed by the free-text entry.
func (m Model) modelEntries() []string {
	query := strings.ToLower(strings.TrimSpace(m.modelQuery))
	var ids []string
	for _, model := range m.modelState.models {
		if query == "" ||
			strings.Contains(strings.ToLower(model.ID), query) ||
			strings.Contains(strings.ToLower(model.DisplayName), query) ||
			strings.Contains(strings.ToLower(model.Name), query) {
			ids = append(ids, model.ID)
		}
	}
	return append(ids, otherModelSentinel)
}

const otherModelSentinel = "\x00other"

func (m Model) handleModelDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modelCustomMode || m.modelState.status == ModelPickerEmpty || m.modelState.status == ModelPickerError {
		switch msg.String() {
		case "esc":
			if m.modelCustomMode {
				m.modelCustomMode = false
				m.modelCustomText = ""
				return m, nil
			}
			return m.closeModelDialog()
		case "enter":
			text := strings.TrimSpace(m.modelCustomText)
			if text == "" {
				return m, nil
			}
			return m.applyModelSelection(text)
		case "backspace":
			if len(m.modelCustomText) > 0 {
				m.modelCustomText = m.modelCustomText[:len(m.modelCustomText)-1]
			}
			return m, nil
		default:
			if len(msg.Runes) > 0 {
				m.modelCustomText += string(msg.Runes)
			}
			return m, nil
		}
	}

	entries := m.modelEntries()
	switch msg.String() {
	case "esc":
		return m.closeModelDialog()
	case "up":
		if len(entries) > 0 {
			m.modelCursor = (m.modelCursor - 1 + len(entries)) % len(entries)
		}
		return m, nil
	case "down":
		if len(entries) > 0 {
			m.modelCursor = (m.modelCursor + 1) % len(entries)
		}
		return m, nil
	case "enter":
		if m.modelCursor >= len(entries) {
			return m, nil
		}
		if entries[m.modelCursor] == otherModelSentinel {
			m.modelCustomMode = true
			m.modelCustomText = strings.TrimSpace(m.modelQuery)
			return m, nil
		}
		return m.applyModelSelection(entries[m.modelCursor])
	case "backspace":
		if len(m.modelQuery) > 0 {
			m.modelQuery = m.modelQuery[:len(m.modelQuery)-1]
			m.modelCursor = 0
		}
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.modelQuery += string(msg.Runes)
			m.modelCursor = 0
		}
		return m, nil
	}
}

func (m Model) applyModelSelection(modelID string) (tea.Model, tea.Cmd) {
	updated := *m.selectedProvider
	updated.Model = modelID
	wd := m.getWorkDir()
	client, err := llm.NewClient(&updated, m.loadSkillsAndBuildPrompt(wd))
	if err != nil {
		m.modelDialog = false
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "error",
			content: fmt.Sprintf("Failed to switch model: %s", err),
		})
		m.updateViewport()
		return m, nil
	}
	m.client = client
	for i := range m.providers {
		if m.providers[i].BaseURL == updated.BaseURL && m.providers[i].Name == updated.Name {
			m.providers[i].Model = modelID
			m.selectedProvider = &m.providers[i]
			break
		}
	}
	if m.ag != nil {
		m.ag.Client = client
		m.ag.ContextWindow = config.GetContextWindow(&updated)
		m.ag.MaxOutputTokens = config.GetMaxOutputTokens(&updated)
		if at, ok := m.registry.Get("Agent").(*subagent.AgentTool); ok {
			at.Client = client
		}
	}
	mcp.DecideAndApply(m.registry, updated.BaseURL, config.GetContextWindow(&updated))
	m.modelDialog = false

	// A save failure must not undo the runtime switch.
	saved := true
	if err := config.PersistModel(updated.BaseURL, updated.Model); err != nil {
		saved = false
	}
	note := fmt.Sprintf("Model set to %s and saved.", updated.Model)
	if !saved {
		note = fmt.Sprintf("Model set to %s for this session.", updated.Model)
	}
	m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: note})
	m.updateViewport()
	return m, nil
}

func (m Model) renderModelDialog() string {
	if !m.modelDialog {
		return ""
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(Theme.BorderAccent)).
		Padding(1, 2)

	var inner strings.Builder
	inner.WriteString(selectLabelStyle.Render("Select model"))
	inner.WriteString("\n")

	switch m.modelState.status {
	case ModelPickerLoading:
		inner.WriteString(permDimStyle.Render("Fetching models…"))
		inner.WriteString("\n")
	case ModelPickerEmpty:
		inner.WriteString(permDimStyle.Render("No models returned by this provider — type a model id"))
		inner.WriteString("\n")
	case ModelPickerError:
		inner.WriteString(permDimStyle.Render("Model discovery failed for this provider — type a model id"))
		inner.WriteString("\n")
	}

	if m.modelCustomMode || m.modelState.status == ModelPickerEmpty || m.modelState.status == ModelPickerError {
		inner.WriteString(fmt.Sprintf("%s %s█\n", promptStyle.Render(">"), m.modelCustomText))
		inner.WriteString(permDimStyle.Render("Enter set model · Esc cancel"))
		return border.Render(inner.String()) + "\n"
	}

	if m.modelState.status == ModelPickerReady {
		entries := m.modelEntries()
		if m.modelCursor >= len(entries) {
			m.modelCursor = 0
		}
		for i, entry := range entries {
			label := entry
			if entry == otherModelSentinel {
				label = "Other (type your own)"
			}
			if i == m.modelCursor {
				inner.WriteString(selectedItemStyle.Render("❯ " + label))
			} else {
				inner.WriteString(normalItemStyle.Render("  " + label))
			}
			inner.WriteString("\n")
		}
		inner.WriteString(permDimStyle.Render(fmt.Sprintf("Filter: %s · ↑↓ navigate · Enter select · Esc cancel", m.modelQuery)))
	}
	return border.Render(inner.String()) + "\n"
}
