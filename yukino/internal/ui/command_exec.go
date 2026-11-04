package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

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
	"github.com/yukino-js/yukino/yukino/internal/workflow"
)

func (m Model) buildCommandContext(args string) *commands.Context {
	wd, _ := os.Getwd()
	modelName := ""
	if m.selectedProvider != nil {
		modelName = m.selectedProvider.Model
	}
	ctx := &commands.Context{
		Args:  args,
		Cwd:   wd,
		Model: modelName,
		MemoryList: func() []string {
			if m.memoryMgr == nil {
				return nil
			}
			memories := m.memoryMgr.GetMemories()
			names := make([]string, 0, len(memories))
			for _, memory := range memories {
				names = append(names, memory.Name)
			}
			return names
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
		PersistThinkingLevel: func(level string) error {
			if m.selectedProvider == nil {
				return fmt.Errorf("no active provider")
			}
			return config.PersistThinkingLevel(m.selectedProvider.BaseURL, level)
		},
	}
	if control, ok := m.client.(llm.ThinkingLevelControl); ok {
		ctx.SetThinkingLevel = func(level string) {
			control.SetThinkingLevel(level)
			if m.selectedProvider != nil {
				m.selectedProvider.Thinking = control.GetThinkingLevel()
			}
		}
	}
	return ctx
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
		return m, nil
	}

	name = cmd.Name
	ctx := m.buildCommandContext(args)

	// The TUI intercepts these commands by name and renders the real status
	// itself; the registry handlers are placeholders.
	switch name {
	case "thinking":
		if strings.TrimSpace(args) == "" {
			if _, ok := m.client.(llm.ThinkingLevelControl); ok {
				return m.openThinkingDialog()
			}
		}
	case "workflows":
		if strings.TrimSpace(args) == "" {
			return m.openWorkflowsDialog()
		}
		if service := workflow.GetWorkflowService(m.registry); service != nil {
			ctx := m.workflowContext()
			return m, func() tea.Msg {
				output, err := service.Command(args, ctx)
				return workflowActionDoneMsg{output: output, err: err}
			}
		}
	case "status":
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: m.buildSessionStatus()})
		return m, nil
	case "memory":
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: m.buildMemoryStatus(args)})
		return m, nil
	case "mcp":
		info := m.mcpServerInfo
		if info == "" {
			info = "No MCP servers connected"
		}
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: info})
		return m, nil
	case "skills":
		if m.skillCatalog == nil {
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "Skills: no catalog loaded."})
			return m, nil
		}
		if strings.TrimSpace(args) == "reload" {
			m.skillCatalog.Reload()
			bootstrap.WireSkillsToRegistry(m.skillCatalog, m.cmdRegistry, m)
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: fmt.Sprintf("Skills reloaded. %d skill(s) available.", len(m.skillCatalog.List()))})
			return m, nil
		}
		list := m.skillCatalog.List()
		if len(list) == 0 {
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "No skills found in .agents/skills/."})
			return m, nil
		}
		var sb strings.Builder
		sb.WriteString("Available skills:\n")
		for _, s := range list {
			sb.WriteString(fmt.Sprintf("  /%s — %s\n", s.Name, s.Description))
		}
		sb.WriteString("\nType /skills reload to hot-reload skills from disk.")
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: sb.String()})
		return m, nil
	case "goal":
		return m.runGoalCommand(args)
	}

	switch cmd.Type {
	case commands.TypeLocalUI:
		switch name {
		case "clear":
			m.pendingMessages = nil
			m.steeringPending = nil
			m.queuePaused = false
			if m.taskMgr != nil {
				if err := m.taskMgr.StopAll(context.Background()); err != nil {
					return m, nil
				}
			}
			m.stopMemoryTasks()
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
			m.interactionStats = bootstrap.InteractionSummary{StartedAt: float64(time.Now().UnixMilli())}
			m.activeToolIDs = nil
			m.agentStartedAt, m.toolBatchStartedAt = time.Time{}, time.Time{}
			m.totalOutput = 0
			m.totalCacheRead = 0
			m.totalCacheCreation = 0
			m.subagents = nil
			m.prepareOutput()
			return m, tea.Batch(
				func() tea.Msg { return tea.ClearScreen() },
				tea.Println(m.renderTranscriptHeader()),
			)
		case "compact":
			if m.client == nil || m.conversation == nil {
				m.chatMessages = append(m.chatMessages, chatMessage{
					role: "error", content: "Compact requires an active provider.",
				})
				return m, nil
			}
			m.chatMessages = append(m.chatMessages, chatMessage{
				role: "system", content: "Compacting conversation…",
			})
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
			compactCtx, cancel := context.WithCancel(context.Background())
			m.compacting = true
			m.cancelStream = cancel
			return m, func() tea.Msg {
				result, err := compact.ForceCompact(compactCtx, conv, client, recovery, names, schemas, sessionFilePath, customInstructions)
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
			return m, nil
		case "quit":
			return m, tea.Quit
		case "model":
			if strings.TrimSpace(args) != "" {
				return m.applyModelSelection(args)
			}
			if m.client == nil {
				m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: ClientNotReady})
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
			return m, nil
		}

	case commands.TypePrompt:
		if cmd.Handler != nil {
			prompt := cmd.Handler(ctx)
			displayText := "/" + name
			if args != "" {
				displayText += " " + args
			}
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
		skillName := name
		skillArgs := args
		commitText := userLine + "\n" + lipgloss.NewStyle().Foreground(dimText).Render(forkNotice)
		return m, tea.Batch(
			tea.Println(commitText),
			func() tea.Msg {
				skill := m.skillCatalog.Get(skillName)
				if skill == nil {
					return forkSkillDoneMsg{name: skillName, err: fmt.Errorf("unknown skill: %s", skillName)}
				}
				result, runErr := skills.RunFork(context.Background(), skill, skillArgs, m)
				return forkSkillDoneMsg{name: skillName, result: result, err: runErr}
			},
		)

	case commands.TypeLocal:
		if cmd.Handler != nil {
			output := cmd.Handler(ctx)
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: output, markdown: cmd.Markdown})
			return m, nil
		}
	}

	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "system",
		content: fmt.Sprintf("/%s — not yet implemented", name),
	})
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
	mgr := memory.NewManager(wd, nil)
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
		lines = append(lines, fmt.Sprintf("  [%s] %s — %s", mem.Type, mem.Name, mem.Description))
	}
	return fmt.Sprintf("Memories (%d):\n%s", len(mems), strings.Join(lines, "\n"))
}
