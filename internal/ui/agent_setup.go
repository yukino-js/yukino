package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/commands"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/hooks"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/mcp"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/memory"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/memory/consolidation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/memory/extractor"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/prompt"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/skills"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/subagent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/teams"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/todo"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/worktree"
)

func (m *Model) drainTaskNotifications() []string {
	var messages []string
	if m.taskMgr != nil {
		for _, n := range m.taskMgr.DrainNotifications() {
			msg := fmt.Sprintf("<task-notification>\n<task_id>%s</task_id>\n<status>%s</status>\n<summary>Agent \"%s\" %s</summary>\n<result>%s</result>\n</task-notification>",
				n.ID, n.Status, n.Name, n.Status, n.Output)
			messages = append(messages, msg)
		}
	}
	messages = append(messages, teams.DrainLeadMailbox(m.teamMgr)...)
	return messages
}

func newAgentHookRunner(client llm.Client) func(prompt string, ctx hooks.HookContext) (string, error) {
	return func(prompt string, _ hooks.HookContext) (string, error) {
		c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		conv := conversation.NewManager()
		conv.AddUserMessage(prompt)
		events, errs := client.Stream(c, conv, nil)
		var text string
		for ev := range events {
			if td, ok := ev.(llm.TextDelta); ok {
				text += td.Text
			}
		}
		select {
		case err := <-errs:
			if err != nil {
				return "", err
			}
		default:
		}
		return text, nil
	}
}

func (m *Model) registerAgentTools(client llm.Client, providerCfg *config.ProviderConfig, protocol, wd string) {
	if server, ok := m.registry.Get("LSP").(*tools.LSPTool); ok {
		server.Configs = m.LSPServers
	}
	if m.taskMgr != nil {
		_ = m.taskMgr.StopAll(context.Background())
	}
	m.taskMgr = subagent.NewTaskManager()
	if err := m.taskMgr.UseSession(m.sessionID); err != nil {
		panic(err)
	}
	tools.AttachBackgroundTaskManager(m.registry, m.taskMgr)
	manager := m.taskMgr
	m.registry.AddCleanup(func() error { return manager.StopAll(context.Background()) })

	store, err := todo.NewStore(m.sessionID)
	if err != nil {
		panic(err)
	}
	err = m.todoList.UseStore(store)
	if err != nil {
		panic(err)
	}

	m.memoryMgr = memory.NewManager(wd)

	loader := subagent.NewAgentLoader()
	loader.LoadAll()

	teamMgr := teams.NewTeamManager(wd)
	teamMgr.CoordinatorMode = m.EnableCoordinatorMode
	m.teamMgr = teamMgr
	m.registry.Register(&teams.TeamTodoWriteTool{TeamMgr: m.teamMgr, Fallback: m.todoList})

	gitRoot := worktree.FindCanonicalGitRoot(wd)
	m.registry.Register(&tools.EnterWorktreeTool{
		SessionID: m.sessionID,
		RepoRoot:  gitRoot,
	})
	m.registry.Register(&tools.ExitWorktreeTool{
		RepoRoot: gitRoot,
	})

	if gitRoot != "" {
		if savedSession, err := worktree.LoadWorktreeSession(gitRoot); err == nil && savedSession != nil {
			if info, err := os.Stat(savedSession.WorktreePath); err == nil && info.IsDir() {
				worktree.RestoreWorktreeSession(savedSession)
			}
		}
	}

	worktree.StartCleanupLoop(context.Background())

	m.registry.Register(&tools.ExitPlanModeTool{})

	m.registry.Register(&tools.ToolSearchTool{Registry: m.registry})
	m.registry.Register(&tools.McpCallTool{Registry: m.registry})
	m.registry.Register(&teams.TeamCreateTool{TeamMgr: teamMgr})
	m.registry.Register(&teams.TeamDeleteTool{TeamMgr: teamMgr})
	m.registry.Register(&teams.TeamListTool{TeamMgr: teamMgr})
	m.registry.Register(&teams.SendMessageTool{TeamMgr: teamMgr, SenderName: "leader"})
	m.registry.Register(&teams.TaskStopTool{TeamMgr: teamMgr, TaskManager: m.taskMgr})
	m.registry.Register(&tools.SyntheticOutputTool{})
	m.registry.Register(&subagent.AgentTool{
		Client:        client,
		Provider:      providerCfg,
		MemoryContent: m.memoryContent,
		Registry:      m.registry,
		Protocol:      protocol,
		TaskMgr:       m.taskMgr,
		ProgressCh:    m.subAgentProgressCh,
		Loader:        loader,
		Conversation:  m.conversation,
		TeamMgr:       teamMgr,
		ForkDisabled:  m.ForkDisabled,
	})

}

func (m *Model) initMCPServersCmd() tea.Cmd {
	if len(m.mcpConfigs) == 0 {
		return nil
	}

	m.mcpConnecting = true
	configs := m.mcpConfigs

	return func() tea.Msg {
		mgr := mcp.NewManager()
		var serverConfigs []mcp.ServerConfig
		for _, c := range configs {
			serverConfigs = append(serverConfigs, mcp.ServerConfig{
				Name:      c.Name,
				Command:   c.Command,
				Args:      c.Args,
				URL:       c.URL,
				Transport: c.Transport,
				Headers:   c.Headers,
				Env:       c.Env,
			})
		}
		mgr.LoadConfigs(serverConfigs)
		return mcpReadyMsg{result: mgr.ConnectAll(context.Background())}
	}
}

func (m *Model) loadSkillsAndBuildPrompt(wd string) string {
	m.skillCatalog = skills.LoadCatalog(wd)

	for _, cmd := range commands.LoadUserCommands() {
		if m.cmdRegistry.Find(cmd.Name) != nil {
			continue
		}
		m.cmdRegistry.Register(cmd)
	}

	return m.rebuildSystemPrompt(wd)
}

func (m *Model) wireSkillsToAgent() {
	if m.skillCatalog == nil || m.ag == nil {
		return
	}
	for _, meta := range m.skillCatalog.List() {
		m.registerSkillCommand(meta.Name)
	}
	m.registry.Register(&skills.LoadSkillTool{
		Catalog:  m.skillCatalog,
		Host:     m,
		ForkHost: m,
	})
	m.registry.Register(&skills.InstallSkillTool{
		Catalog: m.skillCatalog,
		OnInstalled: func(name string) {
			m.registerSkillCommand(name)
		},
	})
}

func (m *Model) registerSkillCommand(name string) {
	if m.skillCatalog == nil || m.cmdRegistry == nil {
		return
	}
	if m.cmdRegistry.Find(name) != nil {
		return
	}
	meta := m.skillCatalog.Get(name)
	if meta == nil {
		return
	}
	cmd := &commands.Command{
		Name:        name,
		Description: meta.Meta.Description + " [skill]",
	}
	if meta.Meta.IsFork() {
		cmd.Type = commands.TypeSkillFork
		cmd.Handler = func(ctx *commands.Context) string {
			return ""
		}
	} else {
		cmd.Type = commands.TypePrompt
		captured := name
		cmd.Handler = func(ctx *commands.Context) string {
			skill, err := m.skillCatalog.GetFull(captured)
			if err != nil && skill == nil {
				return fmt.Sprintf("[skill error] %v", err)
			}
			body, runErr := skills.RunInline(context.Background(), skill, ctx.Args, m)
			if runErr != nil {
				return fmt.Sprintf("[skill error] %v", runErr)
			}
			if m.ag != nil {
				m.ag.RecoveryState.RecordSkillInvocation(skill.Meta.Name, body)
			}
			return body
		}
	}
	m.cmdRegistry.Register(cmd)
}

func (m *Model) refreshSkillsIfNeeded() {
	if m.skillCatalog == nil || m.client == nil {
		return
	}
	if !m.skillCatalog.NeedsReload() {
		return
	}
	wd, _ := os.Getwd()
	m.skillCatalog.Reload(wd)
	for _, meta := range m.skillCatalog.List() {
		m.registerSkillCommand(meta.Name)
	}
}

func (m *Model) skillDelta() string {
	if m.skillCatalog == nil {
		return ""
	}
	if m.announcedSkills == nil {
		m.announcedSkills = map[string]bool{}
	}
	var lines []string
	for _, meta := range m.skillCatalog.List() {
		if m.announcedSkills[meta.Name] {
			continue
		}
		m.announcedSkills[meta.Name] = true
		desc := meta.Description
		if len(desc) > 200 {
			desc = desc[:200] + "…"
		}
		lines = append(lines, fmt.Sprintf("- /%s: %s", meta.Name, desc))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (m *Model) rebuildSystemPrompt(wd string) string {
	m.instructionsContent = m.loadCustomInstructions(wd)
	m.memoryContent = memory.LoadAutoMemoryPrompt(wd)
	env := prompt.DetectEnvironment(wd)
	if m.selectedProvider != nil {
		env.Model = m.selectedProvider.Model
	}
	return prompt.BuildSystemPrompt(env)
}

func (m *Model) buildSkillSection(wd string) string {
	if m.skillCatalog == nil {
		return ""
	}
	metas := m.skillCatalog.List()
	if len(metas) == 0 {
		return ""
	}
	skillsDir := filepath.Join(wd, ".yukino", "skills")
	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString(fmt.Sprintf("Skills are installed at: %s\n", skillsDir))
	sb.WriteString("When creating new skills, always place them under this directory as <skill-name>/SKILL.md.\n\n")
	sb.WriteString("Only Skill names and one-line descriptions are listed below. To activate a Skill on demand call the LoadSkill tool with {name: \"<skill-name>\"}. After activation the Skill's full SOP gets pinned to the environment context, and any tools the Skill declares get registered. Users can also invoke a Skill directly with /<name>.\n\n")
	sb.WriteString("If the user pastes a Skill URL (skills.sh, github.com tree URL, or raw SKILL.md URL) and asks to install / add / get it, call the InstallSkill tool with {url: \"<url>\"} — the new Skill becomes available immediately afterwards.\n\n")
	for _, meta := range metas {
		desc := meta.Description
		if len(desc) > 200 {
			desc = desc[:200] + "…"
		}
		sb.WriteString(fmt.Sprintf("- /%s: %s\n", meta.Name, desc))
	}
	return sb.String()
}

func (m Model) ActivateSkill(name, body string) {
	if m.ag != nil {
		m.ag.ActivateSkill(name, body)
	}
}

func (m Model) SetToolFilter(allow func(name string) bool) {
	if m.ag == nil {
		return
	}
	m.ag.SetToolFilter(allow)
}

func (m Model) ToolRegistry() *tools.Registry {
	return m.registry
}

func (m Model) SnapshotParentMessages() []conversation.Message {
	if m.conversation == nil {
		return nil
	}
	src := m.conversation.GetMessages()
	out := make([]conversation.Message, len(src))
	copy(out, src)
	return out
}

func (m Model) RunSubAgent(ctx context.Context, body string, seed []conversation.Message, _ string) (string, error) {
	if m.client == nil {
		return "", fmt.Errorf("RunSubAgent: no llm client (provider not selected)")
	}
	subConv := conversation.NewManager()
	for _, msg := range seed {
		switch msg.Role {
		case "user":
			subConv.AddUserMessage(msg.Content)
		case "assistant":
			subConv.AddAssistantMessage(msg.Content)
		}
	}
	subConv.AddUserMessage(body)

	subAgent := agent.New(m.client, m.registry, "")
	if m.selectedProvider != nil {
		subAgent.Protocol = m.selectedProvider.Protocol
		subAgent.ContextWindow = config.GetContextWindow(m.selectedProvider)
		subAgent.MaxOutputTokens = config.GetMaxOutputTokens(m.selectedProvider)
	}
	subAgent.MaxIterations = 50

	var output strings.Builder
	ch := subAgent.Run(ctx, subConv)
	for ev := range ch {
		switch e := ev.(type) {
		case agent.StreamText:
			output.WriteString(e.Text)
		case agent.ErrorEvent:
			return output.String(), fmt.Errorf("%s", e.Message)
		}
	}
	return output.String(), nil
}

func (m *Model) loadCustomInstructions(wd string) string {
	return memory.LoadInstructions(wd)
}

func (m *Model) installMemoryExtractor(ag *agent.Agent, wd, protocol string) *extractor.Extractor {
	if m.client == nil || m.conversation == nil {
		return nil
	}
	conv := m.conversation
	extr := extractor.InitExtractMemories(extractor.Deps{
		MemoryDir:     memory.GetAutoMemPath(wd),
		UserMemoryDir: memory.GetUserAutoMemPath(),
		ProjectRoot:   wd,
		Client:        m.client,
		ToolRegistry:  m.registry,
		Protocol:      protocol,
		Conversation:  conv,
		AppendSystem:  func(s string) { conv.AddSystemReminder(s) },
	})

	consolidator := consolidation.NewConsolidator(consolidation.Deps{
		MemoryDir:     memory.GetAutoMemPath(wd),
		UserMemoryDir: memory.GetUserAutoMemPath(),
		ProjectRoot:   wd,
		Client:        m.client,
		ToolRegistry:  m.registry,
		Protocol:      protocol,
		Conversation:  conv,
		AppendSystem:  func(s string) { conv.AddSystemReminder(s) },
	})
	m.memoryConsolidator = consolidator

	ag.OnLoopComplete = func(_ *conversation.Manager) {
		_ = extr.Execute(context.Background())
		consolidator.MaybeRun(context.Background())
	}
	return extr
}

func (m *Model) prefetchRelevantMemories(query string) <-chan agent.RecallResult {
	out := make(chan agent.RecallResult, 1)
	if m.memoryMgr == nil || m.selectedProvider == nil {
		out <- agent.RecallResult{}
		return out
	}
	provider := m.selectedProvider
	memDir := m.memoryMgr.Dir()
	userMemDir := m.memoryMgr.UserDir()
	ag := m.ag
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		selector := func(ctx context.Context, sys, user string) (string, error) {
			sideClient, err := llm.NewClient(provider, sys)
			if err != nil {
				return "", err
			}
			miniConv := conversation.NewManager()
			miniConv.AddUserMessage(user)
			events, errs := sideClient.Stream(ctx, miniConv, nil)
			var sb strings.Builder
			for ev := range events {
				if td, ok := ev.(llm.TextDelta); ok {
					sb.WriteString(td.Text)
				}
			}
			select {
			case err := <-errs:
				if err != nil {
					return "", err
				}
			default:
			}
			return sb.String(), nil
		}
		var recentTools []string
		var surfaced map[string]struct{}
		if ag != nil {
			recentTools, surfaced = ag.RecallHints()
		}
		results, _ := memory.FindRelevantMemories(ctx, query, userMemDir, memDir, recentTools, surfaced, selector)
		paths := make([]string, 0, len(results))
		for _, r := range results {
			paths = append(paths, r.Path)
		}
		out <- agent.RecallResult{Reminder: renderRelevantMemoriesReminder(results), Paths: paths}
	}()
	return out
}

func renderRelevantMemoriesReminder(memories []memory.RelevantMemory) string {
	if len(memories) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("The following relevant memories from prior conversations may help:\n\n")
	for _, mem := range memories {
		data, err := os.ReadFile(mem.Path)
		if err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("## Memory: %s (saved %s)\n\n", filepath.Base(mem.Path), memory.MemoryAge(mem.MtimeMs)))
		if note := memory.MemoryFreshnessText(mem.MtimeMs); note != "" {
			sb.WriteString(note)
			sb.WriteString("\n\n")
		}
		sb.Write(data)
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String()
}

func (m *Model) savePartialResponse() {
	if m.streamBuf != "" {
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "assistant",
			content: m.streamBuf,
		})
		m.conversation.AddAssistantMessage(m.streamBuf)
		wd, _ := os.Getwd()
		session.SaveMessage(wd, m.sessionID, session.Message{
			Role: "assistant", Content: m.streamBuf, Timestamp: time.Now().Unix(),
		})
		m.streamBuf = ""
	}
	m.toolBlocks = nil

	msgs := m.conversation.GetMessages()
	if len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == "assistant" && len(last.ToolUses) > 0 {
			var results []conversation.ToolResultBlock
			for _, tu := range last.ToolUses {
				results = append(results, conversation.ToolResultBlock{
					ToolUseID: tu.ToolUseID,
					Content:   "Tool execution was interrupted by user.",
					IsError:   true,
				})
			}
			m.conversation.AddToolResultsMessage(results)
		}
	}

	m.chatMessages = append(m.chatMessages, chatMessage{
		role:    "system",
		content: "(response interrupted)",
	})
	m.updateViewport()
}

func (m *Model) finishStreaming() {
	if m.thinking {
		m.thinkingDone = time.Since(m.thinkingStart).Seconds()
		m.thinking = false
	}
	m.streaming = false
	m.cancelStream = nil
	m.agentCh = nil
	m.updateViewport()
}
