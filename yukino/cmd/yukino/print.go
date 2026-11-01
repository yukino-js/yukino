package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/file_history"
	"github.com/yukino-js/yukino/yukino/internal/history"
	"github.com/yukino-js/yukino/yukino/internal/hooks"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
	"github.com/yukino-js/yukino/yukino/internal/memory"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/prompt"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
	"github.com/yukino-js/yukino/yukino/internal/teams"
	"github.com/yukino-js/yukino/yukino/internal/todo"
	"github.com/yukino-js/yukino/yukino/internal/tools"
	"github.com/yukino-js/yukino/yukino/internal/worktree"
)

type printResult struct {
	Type       string         `json:"type"`
	Result     string         `json:"result"`
	DurationMs int64          `json:"duration_ms"`
	NumTurns   int            `json:"num_turns"`
	ToolCalls  []toolCallInfo `json:"tool_calls"`
	Usage      usageInfo      `json:"usage"`
	StopReason string         `json:"stop_reason"`
}

type toolCallInfo struct {
	Name    string `json:"name"`
	IsError bool   `json:"is_error"`
}

type usageInfo struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func parsePrintFlags(args []string) (string, string, bool) {
	isPrint := false
	outputFormat := "text"
	var prompt string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p", "--print":
			isPrint = true
		case "--output-format":
			if i+1 < len(args) {
				outputFormat = args[i+1]
				i++
			}
		default:
			if isPrint && prompt == "" && !strings.HasPrefix(args[i], "-") {
				prompt = args[i]
			}
		}
	}

	return prompt, outputFormat, isPrint
}

func runPrint(userPrompt string, cfg *config.AppConfig, hookCfgs []config.HookConfig, outputFormat string) error {
	p := config.ResolveDefaultProvider(cfg.Providers, cfg.DefaultProvider)
	wd, _ := os.Getwd()

	defaultTools := tools.CreateDefaultTools()
	registry := defaultTools.Registry
	defer registry.Dispose()
	if server, ok := registry.Get("LSP").(*tools.LSPTool); ok {
		server.Configs = cfg.LspServers
	}

	skillCatalog := skills.LoadCatalog(wd)
	instructionsContent := loadPrintInstructions(wd)
	memoryContent := memory.LoadAutoMemoryPrompt(wd)
	skillSection := buildPrintSkillSection(skillCatalog)

	env := prompt.DetectEnvironment(wd)
	env.Model = p.Model
	systemPrompt := prompt.BuildSystemPrompt(env)

	client, err := llm.NewClient(p, systemPrompt)
	if err != nil {
		return err
	}

	conv := conversation.NewManager()
	sessionID := session.NewID()
	fh := file_history.New(sessionID)
	defaultTools.EditFile.FileHistory = fh
	defaultTools.WriteFile.FileHistory = fh

	taskMgr := subagent.NewTaskManager()
	if err := taskMgr.UseSession(sessionID); err != nil {
		return err
	}
	defer taskMgr.StopAll(context.Background())
	tools.AttachBackgroundTaskManager(registry, taskMgr)
	store, err := todo.NewStore(sessionID)
	if err != nil {
		return err
	}
	todoList, err := todo.NewTaskList(store)
	if err != nil {
		return err
	}
	loader := subagent.NewAgentLoader()
	loader.LoadAll()

	registry.Register(&tools.ToolSearchTool{Registry: registry})
	registry.Register(&tools.McpCallTool{Registry: registry})

	teamMgr := teams.NewTeamManager(wd)
	teamMgr.CoordinatorMode = cfg.EnableCoordinatorMode
	registry.Register(&teams.TeamTodoWriteTool{TeamMgr: teamMgr, Fallback: todoList})
	registry.Register(&teams.TeamCreateTool{TeamMgr: teamMgr})
	registry.Register(&teams.TeamDeleteTool{TeamMgr: teamMgr})
	registry.Register(&teams.TeamListTool{TeamMgr: teamMgr})
	registry.Register(&teams.SendMessageTool{TeamMgr: teamMgr, SenderName: "leader"})
	registry.Register(&teams.TaskStopTool{TeamMgr: teamMgr, TaskManager: taskMgr})
	registry.Register(&tools.SyntheticOutputTool{})

	if len(cfg.MCPServers) > 0 {
		mcpMgr := mcp.NewManager()
		serverConfigs := make([]mcp.ServerConfig, 0, len(cfg.MCPServers))
		for _, c := range cfg.MCPServers {
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
		mcpMgr.LoadConfigs(serverConfigs)
		for _, e := range mcpMgr.RegisterAllTools(context.Background(), registry) {
			fmt.Fprintf(os.Stderr, "MCP warning: %s\n", e)
		}
		mcp.DecideAndApply(registry, p.BaseURL, p.Protocol, config.GetContextWindow(p), cfg.MCPLoading)
		defer mcpMgr.Shutdown()
	}

	subProgressCh := make(chan subagent.SubAgentProgress, 32)
	registry.Register(&subagent.AgentTool{
		Client:        client,
		Provider:      p,
		MemoryContent: memoryContent,
		Registry:      registry,
		Protocol:      p.Protocol,
		TaskMgr:       taskMgr,
		ProgressCh:    subProgressCh,
		Loader:        loader,
		Conversation:  conv,
		TeamMgr:       teamMgr,
		ForkDisabled:  !cfg.ForkEnabled(),
	})

	ag := agent.New(client, registry, p.Protocol)
	ag.ContextWindow = config.GetContextWindow(p)
	ag.MaxOutputTokens = config.GetMaxOutputTokens(p)
	ag.Instructions = instructionsContent
	ag.MemoryContent = memoryContent
	ag.SkillSection = skillSection
	ag.FileHistory = fh
	ag.SetSessionID(sessionID)

	ag.Checker = permissions.NewChecker(
		permissions.NewPathSandbox(wd, true),
		permissions.NewRuleEngine(wd),
		permissions.ModeBypass,
	)

	if len(hookCfgs) > 0 {
		eng := hooks.NewEngine(hookCfgs)
		ag.Hooks = eng
	}

	teamMgr.SetPermissionChecker(ag.Checker)
	ag.NotificationFn = func() []string {
		var messages []string
		for _, task := range taskMgr.DrainNotifications() {
			messages = append(messages, fmt.Sprintf("<task-notification>\n<task_id>%s</task_id>\n<status>%s</status>\n<result>%s</result>\n</task-notification>", task.ID, task.Status, task.Output))
		}
		return append(messages, teams.DrainLeadMailbox(teamMgr)...)
	}
	ag.ToolNameFilter = teams.CoordinatorToolFilter(cfg.EnableCoordinatorMode)
	ag.CoordinatorActiveFn = teams.CoordinatorActiveFn(cfg.EnableCoordinatorMode)

	if at, ok := registry.Get("Agent").(*subagent.AgentTool); ok {
		at.ParentChecker = ag.Checker
	}

	gitRoot := worktree.FindCanonicalGitRoot(wd)
	registry.Register(&tools.EnterWorktreeTool{SessionID: sessionID, RepoRoot: gitRoot})
	registry.Register(&tools.ExitWorktreeTool{RepoRoot: gitRoot})

	input, err := history.RecordInput(history.InputOptions{Cwd: wd, SessionID: &sessionID, Source: "print", Text: userPrompt})
	if err != nil {
		return err
	}
	recorded, err := session.SaveUserMessage(wd, sessionID, input, session.Message{Content: conversation.ExpandAtRefs(userPrompt, wd)})
	if err != nil {
		return err
	}
	conv.AppendMessages([]conversation.Message{recorded.ToConversation()})
	ctx := context.Background()
	ch := ag.Run(ctx, conv)

	start := time.Now()
	var textBuf string
	var totalInput, totalOutput int
	var toolCalls []toolCallInfo
	isJSON := outputFormat == "stream-json"

	for ev := range ch {
		switch e := ev.(type) {
		case agent.StreamText:
			textBuf += e.Text
			if isJSON {
				emitJSON(map[string]any{"type": "assistant", "text": e.Text})
			}

		case agent.ThinkingText:
			if isJSON {
				emitJSON(map[string]any{"type": "thinking", "text": e.Text})
			}

		case agent.ToolUseEvent:
			toolCalls = append(toolCalls, toolCallInfo{Name: e.ToolName})
			if isJSON {
				emitJSON(map[string]any{
					"type":      "tool_use",
					"tool_name": e.ToolName,
					"tool_id":   e.ToolID,
					"args":      e.Args,
				})
			}

		case agent.ToolResultEvent:
			if len(toolCalls) > 0 {
				toolCalls[len(toolCalls)-1].IsError = e.IsError
			}
			if isJSON {
				emitJSON(map[string]any{
					"type":      "tool_result",
					"tool_name": e.ToolName,
					"tool_id":   e.ToolID,
					"output":    e.Output,
					"is_error":  e.IsError,
					"elapsed":   e.Elapsed.Seconds(),
				})
			}

		case agent.UsageEvent:
			totalInput += e.InputTokens
			totalOutput += e.OutputTokens
			if isJSON {
				emitJSON(map[string]any{
					"type":          "usage",
					"input_tokens":  e.InputTokens,
					"output_tokens": e.OutputTokens,
				})
			}

		case agent.TurnComplete:
			if isJSON {
				emitJSON(map[string]any{"type": "turn_complete", "turn": e.Turn})
			}

		case agent.LoopComplete:
			elapsed := time.Since(start)
			if isJSON {
				emitJSON(printResult{
					Type:       "result",
					Result:     textBuf,
					DurationMs: elapsed.Milliseconds(),
					NumTurns:   e.TotalTurns,
					ToolCalls:  toolCalls,
					Usage:      usageInfo{InputTokens: totalInput, OutputTokens: totalOutput},
					StopReason: "end_turn",
				})
			} else {
				fmt.Print(textBuf)
			}
			return nil

		case agent.ErrorEvent:
			if isJSON {
				emitJSON(map[string]any{"type": "error", "message": e.Message})
			} else {
				fmt.Fprintf(os.Stderr, "Error: %s\n", e.Message)
			}

		case agent.CompactEvent:
			if isJSON {
				emitJSON(map[string]any{"type": "compact", "message": e.Message})
			}

		case agent.RetryEvent:
			if isJSON {
				emitJSON(map[string]any{"type": "retry", "reason": e.Reason})
			}
		}
	}

	return nil
}

func emitJSON(v any) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}

func loadPrintInstructions(wd string) string {
	return memory.LoadInstructions(wd)
}

func buildPrintSkillSection(catalog *skills.Catalog) string {
	if catalog == nil {
		return ""
	}
	metas := catalog.List()
	if len(metas) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	for _, meta := range metas {
		desc := meta.Description
		if len(desc) > 200 {
			desc = desc[:200] + "…"
		}
		sb.WriteString(fmt.Sprintf("- /%s: %s\n", meta.Name, desc))
	}
	return sb.String()
}
