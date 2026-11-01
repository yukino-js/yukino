package ui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/commands"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/compact"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/file_history"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/plan_file"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/prompt"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/skills"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/teams"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/todo"
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
	case "memory":
		memories := []string{}
		if m.memoryMgr != nil {
			memories = m.memoryMgr.GetMemories()
		}
		content := fmt.Sprintf("Auto-memory: %d entries", len(memories))
		if m.memoryMgr != nil {
			content += fmt.Sprintf("\nProject: %s\nUser: %s", m.memoryMgr.Dir(), m.memoryMgr.UserDir())
		}
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: content})
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
			m.conversation = conversation.NewManager()
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
			}
			store, err := todo.NewStore(m.sessionID)
			if err != nil {
				panic(err)
			}
			m.todoList = todo.NewTaskList(store)
			m.totalInput = 0
			m.totalOutput = 0
			m.updateViewport()
			return m, tea.Batch(
				func() tea.Msg { return tea.ClearScreen() },
				tea.Println(m.renderTranscriptHeader()),
			)
		case "plan":
			if m.ag != nil && m.ag.Checker != nil {
				m.prePlanMode = m.ag.Checker.Mode()
				m.ag.Checker.SetMode(permissions.ModePlan)
				planPath := plan_file.GetOrCreatePlanPath()
				m.ag.Checker.PlanFilePath = planPath
				m.chatMessages = append(m.chatMessages, chatMessage{
					role:    "system",
					content: fmt.Sprintf("Entered Plan mode. Plan file: %s\nExplore the codebase and design your approach.", planPath),
				})

				if m.hasExitedPlanMode && plan_file.PlanExists() {
					reentryMsg := prompt.BuildPlanModeReentryReminder(planPath, true)
					if reentryMsg != "" {
						m.chatMessages = append(m.chatMessages, chatMessage{
							role:    "system",
							content: reentryMsg,
						})
					}
					m.hasExitedPlanMode = false
				}
			}
			if args != "" {
				m.updateViewport()
				return m.sendMessage(args)
			}
			m.updateViewport()
			return m, nil
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
			window := 200000
			if m.selectedProvider != nil {
				window = config.GetContextWindow(m.selectedProvider)
			}
			var recovery *compact.RecoveryState
			var schemas []map[string]any
			if m.ag != nil {
				recovery = m.ag.RecoveryState
				schemas = m.ag.Registry.GetAllSchemas(m.ag.Protocol)
			}
			compactWD, _ := os.Getwd()
			compactSessionID := m.sessionID
			return m, func() tea.Msg {
				msg, err := compact.ForceCompact(context.Background(), conv, client, compactWD, compactSessionID, window, recovery, schemas)
				return compactDoneMsg{message: msg, err: err}
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
			newModel, teaCmd := m.sendPromptCommand(displayText, prompt)
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
				if m.ag != nil {
					m.ag.RecoveryState.RecordSkillInvocation(skill.Meta.Name, skill.PromptBody)
				}
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
