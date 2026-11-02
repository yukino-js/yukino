package ui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yukino-js/yukino/yukino/internal/bootstrap"
	"github.com/yukino-js/yukino/yukino/internal/commands"
	"github.com/yukino-js/yukino/yukino/internal/compact"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/file_history"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/memory"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/sandbox"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/teams"
	"github.com/yukino-js/yukino/yukino/internal/todo"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

func (m Model) buildCommandContext(args string) *commands.Context {
	wd, _ := os.Getwd()
	modelName := ""
	if m.selectedProvider != nil {
		modelName = m.selectedProvider.Model
	}
	return &commands.Context{
		Args:  args,
		Cwd:   wd,
		Model: modelName,
		MemoryList: func() []string {
			if m.memoryMgr == nil {
				return nil
			}
			return m.memoryMgr.GetMemories()
		},
		TokenCount: func() (int, int) {
			return m.totalInput, m.totalOutput
		},
		PermissionMode: func() string {
			if m.ag != nil && m.ag.Checker != nil {
				return string(m.ag.Checker.Mode())
			}
			return "default"
		},
		ToolCount: func() int {
			return len(m.registry.ListTools())
		},
		ThinkingLevel: func() string {
			if control, ok := m.client.(llm.ThinkingLevelControl); ok {
				return control.GetThinkingLevel()
			}
			return ""
		},
		AvailableThinkingLevels: func() []string {
			if control, ok := m.client.(llm.ThinkingLevelControl); ok {
				return control.GetSupportedThinkingLevels()
			}
			return nil
		},
		SetThinkingLevel: func(level string) {
			if control, ok := m.client.(llm.ThinkingLevelControl); ok {
				control.SetThinkingLevel(level)
			}
		},
		PersistThinkingLevel: func(level string) error {
			if m.selectedProvider == nil {
				return fmt.Errorf("no active provider")
			}
			return config.PersistThinkingLevel(m.selectedProvider.BaseURL, level)
		},
	}
}

// newUICommandRegistry mirrors the app-level command registration in
// src/ui/app.tsx: /provider is registered by the UI, not the default registry.
func newUICommandRegistry() *commands.Registry {
	registry := commands.CreateDefaultRegistry()
	registry.Register(&commands.Command{
		Name:        "provider",
		Type:        commands.TypeLocalUI,
		Description: "Switch the active provider",
	})
	return registry
}

func (m Model) executeCommand(name, args string) (tea.Model, tea.Cmd) {
	cmd := m.cmdRegistry.Find(name)
	if cmd == nil {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "error",
			content: fmt.Sprintf("Unknown command: /%s — type /help to see available commands", name),
		})
		m.updateViewport()
		return m, nil
	}

	ctx := m.buildCommandContext(args)

	// The TUI intercepts these commands by name and renders the real status
	// itself; the registry handlers are placeholders.
	switch name {
	case "status":
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: m.buildSessionStatus()})
		m.updateViewport()
		return m, nil
	case "memory":
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: m.buildMemoryStatus(args)})
		m.updateViewport()
		return m, nil
	case "mcp":
		info := m.mcpServerInfo
		if info == "" {
			info = "No MCP servers connected"
		}
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: info})
		m.updateViewport()
		return m, nil
	case "skills":
		if strings.TrimSpace(args) == "reload" {
			if m.skillCatalog != nil {
				wd, _ := os.Getwd()
				m.skillCatalog.Reload(wd)
				for _, meta := range m.skillCatalog.List() {
					m.registerSkillCommand(meta.Name)
				}
			}
		}
		var sb strings.Builder
		if m.skillCatalog == nil || len(m.skillCatalog.List()) == 0 {
			sb.WriteString("No skills installed.\n\nAdd skills to .yukino/skills/<skill-name>/SKILL.md")
		} else {
			sb.WriteString(fmt.Sprintf("Available skills (%d):\n\n", len(m.skillCatalog.List())))
			for _, s := range m.skillCatalog.List() {
				desc := s.Description
				if len(desc) > 100 {
					desc = desc[:100] + "…"
				}
				sb.WriteString(fmt.Sprintf("  /%s\n    %s\n\n", s.Name, desc))
			}
			sb.WriteString("Type /<skill-name> to invoke a skill.\n")
			sb.WriteString("Type /skills reload to hot-reload skills from disk.")
		}
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: sb.String()})
		m.updateViewport()
		return m, nil
	case "goal":
		return m.runGoalCommand(args)
	}

	switch cmd.Type {
	case commands.TypeLocalUI:
		switch name {
		case "clear":
			if m.taskMgr != nil {
				if err := m.taskMgr.StopAll(context.Background()); err != nil {
					return m, nil
				}
			}
			m.chatMessages = nil
			m.committedUpTo = 0
			m.conversation.Reset()
			if m.ag != nil {
				m.ag.ClearActiveSkills()
				m.ag.SetToolFilter(teams.CoordinatorToolFilter(m.EnableCoordinatorMode))
			}
			m.sessionID = session.NewID()
			if m.taskMgr != nil {
				if err := m.taskMgr.UseSession(m.sessionID); err != nil {
					m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: err.Error()})
					m.updateViewport()
					return m, nil
				}
			}
			m.fileHistory = file_history.New(m.sessionID)
			m.defaultTools.EditFile.FileHistory = m.fileHistory
			m.defaultTools.WriteFile.FileHistory = m.fileHistory
			if m.ag != nil {
				m.ag.FileHistory = m.fileHistory
				m.ag.SetSessionID(m.sessionID)
				m.ag.GoalManager = m.currentGoal()
			}
			store, err := todo.NewStore(m.sessionID)
			if err != nil {
				panic(err)
			}
			if err := m.todoList.UseStore(store); err != nil {
				panic(err)
			}
			m.totalInput = 0
			m.totalOutput = 0
			m.updateViewport()
			return m, tea.Batch(
				func() tea.Msg { return tea.ClearScreen() },
				tea.Println(m.renderTranscriptHeader()),
			)
		case "compact":
			if m.client == nil || m.conversation == nil {
				m.chatMessages = append(m.chatMessages, chatMessage{
					role: "error", content: "Compact requires an active provider.",
				})
				m.updateViewport()
				return m, nil
			}
			m.chatMessages = append(m.chatMessages, chatMessage{
				role: "system", content: "Compacting conversation…",
			})
			m.updateViewport()
			client := m.client
			conv := m.conversation
			var recovery *compact.RecoveryState
			var schemas []map[string]any
			if m.ag != nil {
				recovery = m.ag.RecoveryState
				schemas = m.ag.Registry.GetAllSchemas(m.ag.Protocol)
			}
			compactWD, _ := os.Getwd()
			compactSessionID := m.sessionID
			names := make([]string, 0, len(schemas))
			for _, schema := range schemas {
				if name := tools.SchemaName(schema); name != "" {
					names = append(names, name)
				}
			}
			sessionFilePath := session.SessionFilePath(compactWD, compactSessionID)
			customInstructions := args
			return m, func() tea.Msg {
				result, err := compact.ForceCompact(context.Background(), conv, client, recovery, names, schemas, sessionFilePath, customInstructions)
				if err == nil && result.Boundary != nil {
					_ = session.SaveCompactBoundary(compactWD, compactSessionID, result.Boundary.Summary, result.Boundary.Keep)
				}
				message := ""
				if err == nil {
					message = result.Message
				}
				return compactDoneMsg{message: message, err: err}
			}
		case "resume":
			return m.handleResume(args)
		case "rewind":
			return m.handleRewind()
		case "sandbox":
			m.sandboxDialog = true
			m.sandboxCursor = 0
			m.updateViewport()
			return m, nil
		case "quit":
			return m, tea.Quit
		case "model":
			if strings.TrimSpace(args) != "" {
				return m.applyModelSelection(args)
			}
			if m.client == nil {
				m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: ClientNotReady})
				m.updateViewport()
				return m, nil
			}
			return m.openModelDialog()
		case "provider":
			return m.openProviderDialog()
		case "login":
			return m.openLoginDialog()
		case "worktree":
			m.chatMessages = append(m.chatMessages, chatMessage{
				role:    "system",
				content: fmt.Sprintf("/%s is not yet supported in this build.", name),
			})
			m.updateViewport()
			return m, nil
		}

	case commands.TypePrompt:
		if cmd.Handler != nil {
			prompt := cmd.Handler(ctx)
			displayText := "/" + name
			if args != "" {
				displayText += " " + args
			}
			m.updateViewport()
			newModel, teaCmd := m.sendPromptCommand(displayText, prompt, cmd.Agent)
			if strings.HasSuffix(cmd.Description, "[skill]") {
				loadedLine := lipgloss.NewStyle().Foreground(dimText).PaddingLeft(2).Render(
					fmt.Sprintf("skill(%s)\nSuccessfully loaded skill", name))
				return newModel, tea.Batch(tea.Println(loadedLine), teaCmd)
			}
			return newModel, teaCmd
		}

	case commands.TypeSkillFork:
		displayText := "/" + name
		if args != "" {
			displayText += " " + args
		}
		m.chatMessages = append(m.chatMessages, chatMessage{role: "user", content: displayText})
		userLine := promptStyle.Render("❯ ") + lipgloss.NewStyle().Foreground(brightText).Bold(true).Render(displayText)
		forkNotice := fmt.Sprintf("Forking skill %s in isolated sub-agent…", name)
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: forkNotice})
		m.committedUpTo = len(m.chatMessages)
		m.updateViewport()
		skillName := name
		skillArgs := args
		commitText := userLine + "\n" + lipgloss.NewStyle().Foreground(dimText).Render(forkNotice)
		return m, tea.Batch(
			tea.Println(commitText),
			func() tea.Msg {
				skill, err := m.skillCatalog.GetFull(skillName)
				if err != nil && skill == nil {
					return forkSkillDoneMsg{name: skillName, err: err}
				}
				m.ActivateSkill(skill.Meta.Name, skill.PromptBody)
				result, runErr := skills.RunFork(context.Background(), skill, skillArgs, m)
				return forkSkillDoneMsg{name: skillName, result: result, err: runErr}
			},
		)

	case commands.TypeLocal:
		if cmd.Handler != nil {
			output := cmd.Handler(ctx)
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: output})
			m.updateViewport()
			return m, nil
		}
	}

	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "system",
		content: fmt.Sprintf("/%s — not yet implemented", name),
	})
	m.updateViewport()
	return m, nil
}

// buildSessionStatus renders the rich /status block (src/ui/app.tsx parity:
// provider, sandbox, MCP, session).
func (m Model) buildSessionStatus() string {
	permMode := string(permissions.ModeDefault)
	sandboxEnabled, sandboxAutoAllow := false, false
	if m.ag != nil && m.ag.Checker != nil {
		permMode = string(m.ag.Checker.Mode())
		sandboxEnabled = m.ag.Checker.SandboxEnabled
		sandboxAutoAllow = m.ag.Checker.SandboxAutoAllow
	}
	var model, providerName, protocol string
	if m.selectedProvider != nil {
		model = m.selectedProvider.Model
		providerName = m.selectedProvider.Name
		protocol = m.selectedProvider.Protocol
	}
	sbStatus := "OFF"
	if sandboxEnabled {
		sb := sandbox.New()
		state := "blocked"
		if sb != nil && sb.Available() {
			state = "ready"
		}
		label := "ON (manual)"
		if sandboxAutoAllow {
			label = "ON (auto-allow)"
		}
		sbStatus = label + ", " + state
	}
	memories := "disabled (enable_memory: false)"
	if m.memoryEnabled {
		count := 0
		if m.memoryMgr != nil {
			count = len(m.memoryMgr.GetMemories())
		}
		memories = strconv.Itoa(count)
	}
	skillCount := 0
	if m.skillCatalog != nil {
		skillCount = len(m.skillCatalog.List())
	}
	mcpServers := 0
	if m.mcpMgr != nil {
		mcpServers = len(m.mcpMgr.ConnectedServers())
	}
	wd, _ := os.Getwd()
	lines := []string{
		"Mode:      " + permMode,
		"Model:     " + model,
		fmt.Sprintf("Provider:  %s (%s)", providerName, protocol),
		fmt.Sprintf("Tokens:    %d in / %d out", m.totalInput, m.totalOutput),
		fmt.Sprintf("Tools:     %d", len(m.registry.ListTools())),
		"Sandbox:   " + sbStatus,
		"Memories:  " + memories,
		fmt.Sprintf("Skills:    %d", skillCount),
		fmt.Sprintf("MCP:       %d server(s), %d tool(s)", mcpServers, bootstrap.CountMcpTools(m.registry)),
		"Session:   " + m.sessionID,
		"Directory: " + wd,
	}
	return strings.Join(lines, "\n")
}

// buildMemoryStatus renders /memory output (src/ui/app.tsx parity).
func (m Model) buildMemoryStatus(args string) string {
	if !m.memoryEnabled {
		return "Auto memory is disabled (enable_memory: false in config.yaml)."
	}
	sub := ""
	if fields := strings.Fields(strings.TrimSpace(args)); len(fields) > 0 {
		sub = fields[0]
	}
	wd, _ := os.Getwd()
	mgr := memory.NewManager(wd)
	if sub == "clear" {
		mgr.Clear()
		return "All memories cleared."
	}
	mems := mgr.GetMemories()
	if len(mems) == 0 {
		return "No memories saved yet. They are auto-extracted; /memory clear wipes them."
	}
	lines := make([]string, 0, len(mems))
	for _, mem := range mems {
		lines = append(lines, "  "+mem)
	}
	return fmt.Sprintf("Memories (%d):\n%s", len(mems), strings.Join(lines, "\n"))
}
