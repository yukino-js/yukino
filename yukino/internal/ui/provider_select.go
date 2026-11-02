package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/file_history"
	"github.com/yukino-js/yukino/yukino/internal/history"
	"github.com/yukino-js/yukino/yukino/internal/hooks"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/sandbox"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
	"github.com/yukino-js/yukino/yukino/internal/teams"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

type initSingleProviderMsg struct{}

// activateInitialProvider wires the client and agent for the first active
// provider: the remembered default at startup, or the entry saved from the
// login form when none was configured.
func (m *Model) activateInitialProvider(p *config.ProviderConfig) tea.Cmd {
	wd, _ := os.Getwd()
	systemPrompt := m.loadSkillsAndBuildPrompt(wd)
	client, err := llm.NewClient(p, systemPrompt)
	if err != nil {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: err.Error()})
		return nil
	}
	m.client = client
	m.fileHistory = file_history.New(m.sessionID)
	m.defaultTools.EditFile.FileHistory = m.fileHistory
	m.defaultTools.WriteFile.FileHistory = m.fileHistory
	m.registerAgentTools(client, p, p.Protocol, wd)
	ag := agent.New(client, m.registry, p.Protocol)
	ag.ContextWindow = config.GetContextWindow(p)
	ag.MaxOutputTokens = config.GetMaxOutputTokens(p)
	ag.Instructions = m.instructionsContent
	ag.MemoryContent = m.memoryContent
	ag.SkillSection = m.skillDelta()
	ag.SkillDeltaFn = m.skillDelta
	ag.FileHistory = m.fileHistory
	ag.SetSessionID(m.sessionID)
	ag.GoalManager = m.currentGoal()
	pathSandbox := permissions.NewPathSandbox(wd, true)
	ag.Checker = permissions.NewChecker(
		pathSandbox,
		permissions.NewRuleEngine(wd),
		m.permissionMode,
	)
	ag.Checker.SandboxEnabled = m.sandboxCfg.SandboxEnabled()
	ag.Checker.SandboxAutoAllow = m.sandboxCfg.SandboxAutoAllow()
	if m.sandboxCfg.SandboxEnabled() {
		sb := sandbox.New()
		if bashTool, ok := m.registry.Get("Bash").(*tools.BashTool); ok {
			bashTool.SandboxRequired = true
			bashTool.Sandbox = sb
			bashTool.SandboxConfig = sandbox.Config{
				AllowWrite:     []string{wd, os.TempDir()},
				DenyWrite:      nil,
				NetworkEnabled: m.sandboxCfg.SandboxNetworkEnabled(),
			}
		}
	}
	ag.NotificationFn = m.drainTaskNotifications
	ag.ToolNameFilter = teams.CoordinatorToolFilter(m.EnableCoordinatorMode)
	ag.CoordinatorActiveFn = teams.CoordinatorActiveFn(m.EnableCoordinatorMode)
	if len(m.hookConfigs) > 0 {
		eng := hooks.NewEngine(m.hookConfigs)
		eng.AgentRunner = newAgentHookRunner(client)
		ag.Hooks = eng
	}
	m.ag = ag
	m.teamMgr.SetPermissionChecker(ag.Checker)
	if at, ok := m.registry.Get("Agent").(*subagent.AgentTool); ok {
		at.ParentChecker = ag.Checker
	}
	m.wireSkillsToAgent()
	m.attachWorkflow()
	m.memoryTasks = m.installMemoryTasks(ag, wd)
	entries, err := history.LoadInputTexts()
	if err != nil {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: "Unable to load input history: " + err.Error()})
	}
	m.historyEntries = entries
	m.state = stateChat
	return m.initMCPServersCmd()
}

// renderProviderSelectView covers the startup state with no configured
// providers: the login form takes over the whole screen.
func (m Model) renderProviderSelectView() string {
	return m.renderLoginFrame(m.height)
}

func (m Model) handleProviderSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		return m, tea.Quit
	}
	return m.handleLoginDialog(msg)
}

func providerFuseKeys() []fuseKey[config.ProviderConfig] {
	return []fuseKey[config.ProviderConfig]{
		{weight: 1, get: func(p config.ProviderConfig) (string, bool) { return p.Name, true }},
		{weight: 1, get: func(p config.ProviderConfig) (string, bool) { return p.Protocol, true }},
		{weight: 1, get: func(p config.ProviderConfig) (string, bool) { return p.BaseURL, true }},
		{weight: 1, get: func(p config.ProviderConfig) (string, bool) { return p.Model, true }},
	}
}

func (m Model) openProviderDialog() (tea.Model, tea.Cmd) {
	if len(m.providers) < 2 {
		if m.selectedProvider != nil {
			m.chatMessages = append(m.chatMessages, chatMessage{
				role:    "system",
				content: fmt.Sprintf("Provider: %s · %s", m.selectedProvider.Name, m.selectedProvider.Model),
			})
		}
		return m, nil
	}
	m.providerDialog = true
	m.providerQuery = ""
	m.providerFocusURL = ""
	m.providerFocusSet = false
	if m.selectedProvider != nil {
		m.providerFocusURL = m.selectedProvider.BaseURL
		m.providerFocusSet = true
	}
	return m, nil
}

func (m Model) providerDialogMatches() []config.ProviderConfig {
	query := strings.TrimSpace(m.providerQuery)
	if query == "" {
		return m.providers
	}
	return NewFuse(m.providers, providerFuseKeys(), 0.35, true).Search(query)
}

func (m Model) providerDialogCursor(matches []config.ProviderConfig) int {
	if m.providerFocusSet {
		for i, p := range matches {
			if p.BaseURL == m.providerFocusURL {
				return i
			}
		}
	}
	return 0
}

func (m Model) handleProviderDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := m.providerDialogMatches()
	switch msg.String() {
	case "esc":
		m.providerDialog = false
		return m, nil
	case "up", "down":
		if len(matches) > 0 {
			cursor := m.providerDialogCursor(matches)
			next := cursor - 1
			if msg.String() == "down" {
				next = cursor + 1
			}
			next = (next + len(matches)) % len(matches)
			m.providerFocusURL = matches[next].BaseURL
			m.providerFocusSet = true
		}
		return m, nil
	case "enter":
		if len(matches) > 0 {
			return m.switchProvider(matches[m.providerDialogCursor(matches)])
		}
		return m, nil
	default:
		nextQuery := updateSelectorQuery(m.providerQuery, msg)
		if nextQuery != m.providerQuery {
			m.providerQuery = nextQuery
			if strings.TrimSpace(nextQuery) != "" {
				m.providerFocusSet = false
			} else if m.selectedProvider != nil {
				m.providerFocusURL = m.selectedProvider.BaseURL
				m.providerFocusSet = true
			}
		}
		return m, nil
	}
}

func (m Model) renderProviderDialog() string {
	if !m.providerDialog {
		return ""
	}
	matches := m.providerDialogMatches()
	cursor := m.providerDialogCursor(matches)
	emptyText := "No providers configured"
	if strings.TrimSpace(m.providerQuery) != "" {
		emptyText = "No matching providers"
	}
	return renderSelectorList(selectorListParams{
		title:        "Select provider",
		hint:         "↑↓ navigate · Enter select · Esc cancel · Ctrl+U clear",
		query:        m.providerQuery,
		emptyText:    emptyText,
		itemCount:    len(matches),
		itemHeight:   1,
		totalCount:   len(m.providers),
		cursor:       cursor,
		searchActive: true,
		width:        m.width,
		rows:         m.dialogAvailableRows(),
		renderRows: func(start, count, width int) string {
			var sb strings.Builder
			for i := start; i < start+count && i < len(matches); i++ {
				p := matches[i]
				current := m.selectedProvider != nil && p.BaseURL == m.selectedProvider.BaseURL
				sb.WriteString(renderSelectorListRow(selectorListRowParams{
					label:       p.Name,
					description: p.Protocol + " · " + p.Model,
					focused:     i == cursor,
					current:     current,
					width:       width,
				}))
			}
			return sb.String()
		},
	})
}

// switchProvider mirrors the in-chat branch of handleProviderSelect in
// src/ui/app.tsx.
func (m Model) switchProvider(provider config.ProviderConfig) (tea.Model, tea.Cmd) {
	m.providerDialog = false
	wd := m.getWorkDir()
	client, err := llm.NewClient(&provider, m.loadSkillsAndBuildPrompt(wd))
	if err != nil {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "error",
			content: fmt.Sprintf("Failed to switch provider: %s", err),
		})
		return m, nil
	}
	m.client = client
	for i := range m.providers {
		if m.providers[i].BaseURL == provider.BaseURL {
			m.selectedProvider = &m.providers[i]
			_ = config.PersistDefaultProvider(i)
			break
		}
	}
	if m.ag != nil {
		m.ag.Client = client
		m.ag.ContextWindow = config.GetContextWindow(&provider)
		m.ag.MaxOutputTokens = config.GetMaxOutputTokens(&provider)
		if at, ok := m.registry.Get("Agent").(*subagent.AgentTool); ok {
			at.Client = client
		}
	}
	mcp.DecideAndApply(m.registry, provider.BaseURL, provider.Protocol, config.GetContextWindow(&provider), m.MCPLoading)
	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "system",
		content: fmt.Sprintf("Provider switched to %s · %s.", provider.Name, provider.Model),
	})
	return m, nil
}
