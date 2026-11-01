package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/file_history"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/history"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/hooks"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/sandbox"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/subagent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/teams"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

type initSingleProviderMsg struct{}

func (m Model) handleProviderSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.providerCursor > 0 {
			m.providerCursor--
		}
	case "down", "j":
		if m.providerCursor < len(m.providers)-1 {
			m.providerCursor++
		}
	case "enter":
		p := &m.providers[m.providerCursor]
		m.selectedProvider = p

		_ = config.PersistDefaultProvider(m.providerCursor)
		wd, _ := os.Getwd()
		systemPrompt := m.loadSkillsAndBuildPrompt(wd)
		client, err := llm.NewClient(p, systemPrompt)
		if err != nil {
			m.state = stateChat
			m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: err.Error()})
			return m, nil
		}
		m.client = client
		m.sessionID = session.NewID()
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
		pathSandbox2 := permissions.NewPathSandbox(wd, true)
		ag.Checker = permissions.NewChecker(
			pathSandbox2,
			permissions.NewRuleEngine(wd),
			permissions.ModeDefault,
		)
		ag.Checker.SandboxEnabled = m.sandboxCfg.SandboxEnabled()
		ag.Checker.SandboxAutoAllow = m.sandboxCfg.SandboxAutoAllow()
		if m.sandboxCfg.SandboxEnabled() {
			sb := sandbox.New()
			if bashTool, ok := m.registry.Get("Bash").(*tools.BashTool); ok && sb != nil {
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
		if at, ok := m.registry.Get("Agent").(*subagent.AgentTool); ok {
			at.ParentChecker = ag.Checker
		}
		m.wireSkillsToAgent()
		m.memoryExtractor = m.installMemoryExtractor(ag, wd, p.Protocol)
		m.historyEntries = history.LoadInputTexts()
		m.state = stateChat
		m.textarea.Focus()
		m.updateViewport()
		return m, m.initMCPServersCmd()
	}
	return m, nil
}

func (m Model) renderProviderSelectView() string {
	var sb strings.Builder

	sb.WriteString(selectLabelStyle.Render("Select a Provider"))
	sb.WriteString("\n\n")

	for i, p := range m.providers {
		if i == m.providerCursor {
			sb.WriteString(selectedItemStyle.Render(fmt.Sprintf("  ❯ %s  [%s]", p.Name, p.Model)))
		} else {
			sb.WriteString(normalItemStyle.Render(fmt.Sprintf("    %s  [%s]", p.Name, p.Model)))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
