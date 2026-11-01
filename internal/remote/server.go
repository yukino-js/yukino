package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/commands"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/compact"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/file_history"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/hooks"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/mcp"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/memory"
	extractor "github.com/hangtiancheng/yukino.go/apps/yukino/internal/memory/extractor"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/plan_file"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/prompt"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/skills"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/subagent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/teams"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/todo"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/worktree"
)

type wsMessage struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type clientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type userMessageData struct {
	Content string `json:"content"`
}

type permResponseData struct {
	ID       string `json:"id"`
	Response string `json:"response"`
	Feedback string `json:"feedback,omitempty"`
}

type askUserResponseData struct {
	ID      string            `json:"id"`
	Answers map[string]string `json:"answers"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	MCPLoading string
	providers  []config.ProviderConfig
	mcpConfigs []config.MCPServerConfig
	hookCfgs   []config.HookConfig
	addr       string

	defaultProvider int

	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}

	ag           *agent.Agent
	conv         *conversation.Manager
	registry     *tools.Registry
	defaultTools tools.DefaultTools
	client       llm.Client
	sessionID    string
	fileHistory  *file_history.History

	streaming    bool
	cancelStream context.CancelFunc
	agentCh      <-chan agent.AgentEvent

	askUserCh chan tools.AskUserRequest

	pendingPermMu sync.Mutex
	pendingPerms  map[string]chan<- agent.PermissionReply

	pendingAskMu sync.Mutex
	pendingAsks  map[string]chan tools.QuestionResponse

	cmdRegistry     *commands.Registry
	skillCatalog    *skills.Catalog
	taskMgr         *subagent.TaskManager
	todoList        *todo.TaskList
	memoryMgr       *memory.Manager
	memoryExtractor *extractor.Extractor
	teamMgr         *teams.TeamManager
	mcpMgr          *mcp.Manager

	instructionsContent   string
	memoryContent         string
	mcpInstructions       string
	enableCoordinatorMode bool
	forkDisabled          bool
}

func NewServer(providers []config.ProviderConfig, mcpConfigs []config.MCPServerConfig, hookCfgs []config.HookConfig, addr string, enableCoordinatorMode, forkDisabled bool, defaultProvider int) *Server {
	return &Server{
		providers:             providers,
		mcpConfigs:            mcpConfigs,
		hookCfgs:              hookCfgs,
		addr:                  addr,
		defaultProvider:       defaultProvider,
		enableCoordinatorMode: enableCoordinatorMode,
		forkDisabled:          forkDisabled,
		conns:                 make(map[*websocket.Conn]struct{}),
		pendingPerms:          make(map[string]chan<- agent.PermissionReply),
		pendingAsks:           make(map[string]chan tools.QuestionResponse),
	}
}

func (s *Server) activeProvider() *config.ProviderConfig {
	return config.ResolveDefaultProvider(s.providers, s.defaultProvider)
}

func (s *Server) Run() error {
	defer func() {
		if s.registry != nil {
			_ = s.registry.Dispose()
		}
	}()
	if err := s.initAgent(); err != nil {
		return fmt.Errorf("failed to initialize agent: %w", err)
	}

	s.initMCPServers()

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/ws", s.handleWS)

	fmt.Printf("\n  🌐 Remote UI: http://localhost%s\n\n", s.addr)
	return http.ListenAndServe(s.addr, mux)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()

	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()

	conn.SetReadLimit(4 << 20)

	s.send(wsMessage{Type: "connected", Data: map[string]string{
		"session": s.sessionID,
		"cwd":     mustGetwd(),
	}})

	s.send(wsMessage{Type: "commands", Data: s.buildCommandList()})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		var msg clientMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "user_message":
			var data userMessageData
			json.Unmarshal(msg.Data, &data)
			go s.handleUserMessage(data.Content)

		case "permission_response":
			var data permResponseData
			json.Unmarshal(msg.Data, &data)
			s.handlePermissionResponse(data)

		case "ask_user_response":
			var data askUserResponseData
			json.Unmarshal(msg.Data, &data)
			s.handleAskUserResponse(data)

		case "cancel":
			if s.cancelStream != nil {
				s.cancelStream()
			}

		case "ping":
			s.send(wsMessage{Type: "pong", Data: nil})
		}
	}
}

func (s *Server) initAgent() error {
	p := s.activeProvider()
	wd, _ := os.Getwd()

	s.askUserCh = make(chan tools.AskUserRequest, 1)
	s.defaultTools = tools.CreateDefaultToolsWithWorkDir(wd)
	s.registry = s.defaultTools.Registry
	s.registry.Register(&tools.AskUserQuestionTool{RequestCh: s.askUserCh})

	s.cmdRegistry = commands.CreateDefaultRegistry()
	s.skillCatalog = skills.LoadCatalog(wd)
	s.instructionsContent = loadCustomInstructions(wd)
	s.memoryContent = memory.LoadAutoMemoryPrompt(wd)

	skillSection := buildSkillSection(s.skillCatalog, wd)

	env := prompt.DetectEnvironment(wd)
	env.Model = p.Model
	systemPrompt := prompt.BuildSystemPrompt(env, prompt.BuildOptions{})

	client, err := llm.NewClient(p, systemPrompt)
	if err != nil {
		return err
	}
	s.client = client
	s.conv = conversation.NewManager()
	s.sessionID = session.NewID()
	s.fileHistory = file_history.New(s.sessionID)
	s.defaultTools.EditFile.FileHistory = s.fileHistory
	s.defaultTools.WriteFile.FileHistory = s.fileHistory

	s.registerTools(client, p, wd)

	ag := agent.New(client, s.registry, p.Protocol)
	ag.ContextWindow = config.GetContextWindow(p)
	ag.MaxOutputTokens = config.GetMaxOutputTokens(p)
	ag.Instructions = s.instructionsContent
	ag.MemoryContent = s.memoryContent
	ag.SkillSection = skillSection
	ag.FileHistory = s.fileHistory
	ag.SetSessionID(s.sessionID)

	ag.Checker = permissions.NewChecker(
		permissions.NewPathSandbox(wd, true),
		permissions.NewRuleEngine(wd),
		permissions.ModeDefault,
	)

	if len(s.hookCfgs) > 0 {
		eng := hooks.NewEngine(s.hookCfgs)
		eng.AgentRunner = newAgentHookRunner(client)
		ag.Hooks = eng
	}

	ag.NotificationFn = func() []string {
		var messages []string
		if s.taskMgr != nil {
			for _, n := range s.taskMgr.DrainNotifications() {
				messages = append(messages, fmt.Sprintf(
					"<task-notification>\n<task_id>%s</task_id>\n<status>%s</status>\n<summary>Agent \"%s\" %s</summary>\n<result>%s</result>\n</task-notification>",
					n.ID, n.Status, n.Name, n.Status, n.Output))
			}
		}
		return append(messages, teams.DrainLeadMailbox(s.teamMgr)...)
	}
	ag.ToolNameFilter = teams.CoordinatorToolFilter(s.enableCoordinatorMode)
	ag.CoordinatorActiveFn = teams.CoordinatorActiveFn(s.enableCoordinatorMode)

	s.ag = ag

	if at, ok := s.registry.Get("Agent").(*subagent.AgentTool); ok {
		at.ParentChecker = ag.Checker
	}

	s.wireSkillsToAgent(wd)
	s.memoryExtractor = installMemExtractor(ag, wd, p.Protocol, client, s.registry, s.conv)

	gitRoot := worktree.FindCanonicalGitRoot(wd)
	s.registry.Register(&tools.EnterWorktreeTool{SessionID: s.sessionID, RepoRoot: gitRoot})
	s.registry.Register(&tools.ExitWorktreeTool{RepoRoot: gitRoot})
	worktree.StartCleanupLoop(context.Background())

	return nil
}

func (s *Server) registerTools(client llm.Client, p *config.ProviderConfig, wd string) {
	s.taskMgr = subagent.NewTaskManager()
	if err := s.taskMgr.UseSession(s.sessionID); err != nil {
		panic(err)
	}
	tools.AttachBackgroundTaskManager(s.registry, s.taskMgr)
	manager := s.taskMgr
	s.registry.AddCleanup(func() error { return manager.StopAll(context.Background()) })
	store, err := todo.NewStore(s.sessionID)
	if err != nil {
		panic(err)
	}
	s.todoList = todo.NewTaskList(store)
	s.memoryMgr = memory.NewManager(wd)
	loader := subagent.NewAgentLoader(wd)
	loader.LoadAll()
	s.teamMgr = teams.NewTeamManager(wd)

	s.registry.Register(&tools.ExitPlanModeTool{
		IsPlanMode: func() bool {
			return s.ag != nil && s.ag.Checker != nil && s.ag.Checker.Mode() == permissions.ModePlan
		},
		PlanExists: func() bool { return false },
	})
	s.registry.Register(&todo.TaskCreateTool{List: s.todoList})
	s.registry.Register(&todo.TaskGetTool{List: s.todoList})
	s.registry.Register(&todo.TaskListTool{List: s.todoList})
	s.registry.Register(&todo.TaskUpdateTool{List: s.todoList})
	s.registry.Register(&tools.ToolSearchTool{Registry: s.registry})
	s.registry.Register(&tools.McpCallTool{Registry: s.registry})
	s.registry.Register(&teams.TeamCreateTool{TeamMgr: s.teamMgr})
	s.registry.Register(&teams.TeamDeleteTool{TeamMgr: s.teamMgr})
	s.registry.Register(&teams.SendMessageTool{TeamMgr: s.teamMgr, SenderName: "lead"})
	s.registry.Register(&teams.TaskStopTool{TeamMgr: s.teamMgr, TaskManager: s.taskMgr})
	s.registry.Register(&tools.SyntheticOutputTool{})
	subProgressCh := make(chan subagent.SubAgentProgress, 32)
	s.registry.Register(&subagent.AgentTool{
		Client:        client,
		ModelResolver: llm.NewModelResolver(*p),
		Registry:      s.registry,
		Protocol:      p.Protocol,
		TaskMgr:       s.taskMgr,
		ProgressCh:    subProgressCh,
		Loader:        loader,
		Conversation:  s.conv,
		TeamMgr:       s.teamMgr,
		ForkDisabled:  s.forkDisabled,
	})
}

func (s *Server) initMCPServers() {
	if len(s.mcpConfigs) == 0 {
		return
	}
	mgr := mcp.NewManager()
	var serverConfigs []mcp.ServerConfig
	for _, c := range s.mcpConfigs {
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
	result := mgr.ConnectAll(context.Background())
	s.mcpMgr = result.Mgr
	for _, t := range result.Tools {
		s.registry.Register(t)
	}
	for _, errMsg := range result.Errors {
		log.Printf("MCP error: %s", errMsg)
	}
	if len(s.providers) > 0 {
		p := s.activeProvider()
		mcp.DecideAndApply(s.registry, p.BaseURL, p.Protocol, config.GetContextWindow(p), s.MCPLoading)
	}
	if len(result.Servers) > 0 {
		toolsByServer := make(map[string][]string)
		for _, t := range result.Tools {
			toolName := t.Name()
			for _, srv := range result.Servers {
				if strings.HasPrefix(toolName, mcp.MCPToolNamePrefix(srv.Name)) {
					toolsByServer[srv.Name] = append(toolsByServer[srv.Name], toolName)
					break
				}
			}
		}
		var mcpParts []string
		for _, srv := range result.Servers {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## %s\n", srv.Name))
			if srv.Instructions != "" {
				sb.WriteString(srv.Instructions + "\n")
			}
			if toolNames, ok := toolsByServer[srv.Name]; ok && len(toolNames) > 0 {
				sb.WriteString("\nAvailable tools: " + strings.Join(toolNames, ", "))
			}
			mcpParts = append(mcpParts, sb.String())
		}
		s.mcpInstructions = "# MCP Server Instructions\n\nThe following MCP servers are connected. Use their tools when the user asks.\n\n" + strings.Join(mcpParts, "\n\n")
	}
}

func (s *Server) handleUserMessage(content string) {
	if s.streaming {
		return
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	if strings.HasPrefix(content, "/") {
		s.handleSlashCommand(content)
		return
	}

	s.streaming = true
	wd, _ := os.Getwd()
	session.SaveMessage(wd, s.sessionID, session.Message{Role: "user", Content: content, Ts: time.Now().Unix()})
	s.conv.AddUserMessage(content)

	if s.mcpInstructions != "" {
		s.conv.AddSystemReminder(s.mcpInstructions)
		s.mcpInstructions = ""
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelStream = cancel
	s.agentCh = s.ag.Run(ctx, s.conv)

	askDone := make(chan struct{})
	go s.listenForAskUser(askDone)
	s.consumeAgentEvents()
	close(askDone)

	s.streaming = false
	s.cancelStream = nil
	s.agentCh = nil
}

func (s *Server) buildCommandList() []map[string]string {
	var list []map[string]string
	for _, cmd := range s.cmdRegistry.ListCommands() {
		list = append(list, map[string]string{
			"name":        cmd.Name,
			"description": cmd.Description,
		})
	}
	return list
}

func (s *Server) handleSlashCommand(input string) {
	defer func() {
		if r := recover(); r != nil {
			s.send(wsMessage{Type: "error", Data: map[string]string{
				"message": fmt.Sprintf("Command panic: %v", r),
			}})
		}
	}()

	name, args, ok := commands.Parse(input)
	if !ok {
		return
	}

	cmd := s.cmdRegistry.Find(name)
	if cmd == nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{
			"message": fmt.Sprintf("Unknown command: /%s — type /help to see available commands", name),
		}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	}

	ctx := s.buildCommandContext(args)

	// The remote intercepts these commands by name and renders the real
	// status itself; the registry handlers are placeholders.
	switch name {
	case "memory":
		count := 0
		if s.memoryMgr != nil {
			count = len(s.memoryMgr.GetMemories())
		}
		s.send(wsMessage{Type: "system", Data: map[string]string{
			"message": fmt.Sprintf("Auto-memory: %d entries", count),
		}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	case "mcp":
		info := "No MCP servers connected"
		if s.mcpMgr != nil {
			info = "MCP connected"
		}
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": info}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	}

	switch cmd.Type {
	case commands.TypeLocal:
		if cmd.Handler != nil {
			result := cmd.Handler(ctx)
			s.send(wsMessage{Type: "system", Data: map[string]string{"message": result}})
		}
		s.send(wsMessage{Type: "command_done", Data: nil})

	case commands.TypeLocalUI:
		switch name {
		case "clear":
			s.conv = conversation.NewManager()
			if s.ag != nil {
				s.ag.ClearActiveSkills()
				s.ag.SetToolFilter(teams.CoordinatorToolFilter(s.enableCoordinatorMode))
			}
			s.send(wsMessage{Type: "clear", Data: nil})

		case "compact":
			s.handleCompact()
			return

		case "plan":
			s.handlePlan(args)
			if args != "" {
				return
			}

		case "resume":
			s.handleResume(args)
			return

		case "rewind":
			s.send(wsMessage{Type: "system", Data: map[string]string{
				"message": "Rewind is not yet supported in remote mode.",
			}})
		case "login", "model", "worktree", "skills":
			s.send(wsMessage{Type: "system", Data: map[string]string{
				"message": fmt.Sprintf("/%s is not yet supported in remote mode.", name),
			}})
		case "quit":
			s.send(wsMessage{Type: "system", Data: map[string]string{
				"message": "Quit does not apply to the remote server.",
			}})
		}
		s.send(wsMessage{Type: "command_done", Data: nil})

	case commands.TypePrompt:
		if cmd.Handler == nil {
			return
		}
		prompt := cmd.Handler(ctx)
		displayText := "/" + name
		if args != "" {
			displayText += " " + args
		}
		s.streaming = true
		wd, _ := os.Getwd()
		session.SaveMessage(wd, s.sessionID, session.Message{Role: "user", Content: displayText, Ts: time.Now().Unix()})
		s.conv.AddUserMessage(prompt)

		if s.mcpInstructions != "" {
			s.conv.AddSystemReminder(s.mcpInstructions)
			s.mcpInstructions = ""
		}

		c, cancel := context.WithCancel(context.Background())
		s.cancelStream = cancel
		s.agentCh = s.ag.Run(c, s.conv)

		askDone := make(chan struct{})
		go s.listenForAskUser(askDone)
		s.consumeAgentEvents()
		close(askDone)

		s.streaming = false
		s.cancelStream = nil
		s.agentCh = nil
	}
}

func (s *Server) buildCommandContext(args string) *commands.Context {
	wd, _ := os.Getwd()
	modelName := ""
	if p := s.activeProvider(); p != nil {
		modelName = p.Model
	}
	return &commands.Context{
		Args:       args,
		Cwd:        wd,
		Model:      modelName,
		TokenCount: func() (int, int) { return 0, 0 },
		PermissionMode: func() string {
			if s.ag != nil && s.ag.Checker != nil {
				return string(s.ag.Checker.Mode())
			}
			return "default"
		},
		ToolCount: func() int { return len(s.registry.ListTools()) },
		MemoryList: func() []string {
			if s.memoryMgr == nil {
				return nil
			}
			return s.memoryMgr.GetMemories()
		},
	}
}

func (s *Server) handleCompact() {
	if s.client == nil || s.conv == nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{"message": "Compact requires an active provider."}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	}
	s.send(wsMessage{Type: "system", Data: map[string]string{"message": "Compacting conversation…"}})
	wd, _ := os.Getwd()
	window := config.GetContextWindow(s.activeProvider())
	var recovery *compact.RecoveryState
	var schemas []map[string]any
	if s.ag != nil {
		recovery = s.ag.RecoveryState
		schemas = s.ag.Registry.GetAllSchemas(s.ag.Protocol)
	}
	msg, err := compact.ForceCompact(context.Background(), s.conv, s.client, wd, s.sessionID, window, recovery, schemas)
	if err != nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
	} else {
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": "⟳ " + msg}})
	}
	s.send(wsMessage{Type: "command_done", Data: nil})
}

func (s *Server) handlePlan(args string) {
	wd, _ := os.Getwd()
	if s.ag == nil || s.ag.Checker == nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{"message": "Agent not initialized."}})
		return
	}
	s.ag.Checker.SetMode(permissions.ModePlan)
	planPath := plan_file.GetOrCreatePlanPath()
	s.ag.Checker.PlanFilePath = planPath
	s.send(wsMessage{Type: "system", Data: map[string]string{
		"message": fmt.Sprintf("Entered Plan mode. Plan file: %s\nExplore the codebase and design your approach.", planPath),
	}})

	if args != "" {
		s.streaming = true
		session.SaveMessage(wd, s.sessionID, session.Message{Role: "user", Content: "/plan " + args, Ts: time.Now().Unix()})
		s.conv.AddUserMessage(args)
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelStream = cancel
		s.agentCh = s.ag.Run(ctx, s.conv)
		askDone := make(chan struct{})
		go s.listenForAskUser(askDone)
		s.consumeAgentEvents()
		close(askDone)
		s.streaming = false
		s.cancelStream = nil
		s.agentCh = nil
	}
}

func (s *Server) handleResume(args string) {
	wd, _ := os.Getwd()
	sessions := session.ListSessions(wd)

	if args == "" {
		if len(sessions) == 0 {
			s.send(wsMessage{Type: "system", Data: map[string]string{"message": "No previous sessions found."}})
			s.send(wsMessage{Type: "command_done", Data: nil})
			return
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Available sessions (%d):\n\n", len(sessions)))
		for i, sess := range sessions {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("  … and %d more\n", len(sessions)-20))
				break
			}
			first := sess.FirstMessage
			if len(first) > 60 {
				first = first[:60] + "…"
			}
			sb.WriteString(fmt.Sprintf("  %d. [%s] %s (%d msgs)\n", i+1, sess.ID, first, sess.MessageCount))
		}
		sb.WriteString("\nUsage: /resume <number> or /resume <session-id>")
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": sb.String()}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	}

	targetID := strings.TrimSpace(args)
	var idx int
	if n, _ := fmt.Sscanf(targetID, "%d", &idx); n == 1 && idx >= 1 && idx <= len(sessions) {
		targetID = sessions[idx-1].ID
	}

	msgs := session.LoadSession(wd, targetID)
	if len(msgs) == 0 {
		s.send(wsMessage{Type: "error", Data: map[string]string{
			"message": fmt.Sprintf("Session '%s' not found or empty.", targetID),
		}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	}

	if s.taskMgr != nil {
		if err := s.taskMgr.StopAll(context.Background()); err != nil {
			return
		}
		if err := s.taskMgr.UseSession(targetID); err != nil {
			s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
			return
		}
	}
	s.conv = conversation.NewManager()
	s.sessionID = targetID
	if s.ag != nil {
		s.ag.SetSessionID(s.sessionID)
	}

	boundary, after, compacted := session.FindLastCompactBoundary(msgs)
	var replay []session.Message
	if compacted {
		resumeSummary := fmt.Sprintf("The conversation history before this point was compacted into the following summary:\n\n<summary>\n%s\n</summary>", boundary.Summary)
		if len(boundary.Keep) > 0 {
			resumeSummary += "\n\nRecent messages have been preserved verbatim."
		}
		replay = append(replay, session.Message{Role: "user", Content: resumeSummary})
		for _, k := range boundary.Keep {
			replay = append(replay, session.Message{
				Role:        k.Role,
				Content:     k.Content,
				ToolUses:    k.ToolUses,
				ToolResults: k.ToolResults,
			})
		}
		replay = append(replay, after...)
	} else {
		replay = msgs
	}

	s.send(wsMessage{Type: "clear", Data: nil})
	for _, msg := range replay {
		s.conv.AppendMessages([]conversation.Message{msg.ToConversation()})
		if msg.Content == "" {
			continue
		}
		switch msg.Role {
		case "user":
			s.send(wsMessage{Type: "replay_user", Data: map[string]string{"content": msg.Content}})
		case "assistant":
			s.send(wsMessage{Type: "replay_assistant", Data: map[string]string{"content": msg.Content}})
		}
	}

	restored := fmt.Sprintf("Session %s restored (%d messages).", targetID, len(replay))
	if compacted {
		restored = fmt.Sprintf("Session %s restored from compacted state (summary + %d kept + %d newer).",
			targetID, len(boundary.Keep), len(after))
	}
	s.send(wsMessage{Type: "system", Data: map[string]string{"message": restored}})
	s.send(wsMessage{Type: "command_done", Data: nil})
}

func (s *Server) consumeAgentEvents() {
	streamBuf := ""
	startTime := time.Now()

	for ev := range s.agentCh {
		switch e := ev.(type) {
		case agent.StreamText:
			streamBuf += e.Text
			s.send(wsMessage{Type: "stream_text", Data: map[string]string{"text": e.Text}})

		case agent.ThinkingText:
			s.send(wsMessage{Type: "thinking_text", Data: map[string]string{"text": e.Text}})

		case agent.ToolUseEvent:
			s.send(wsMessage{Type: "tool_use", Data: map[string]any{
				"toolId":   e.ToolID,
				"toolName": e.ToolName,
				"args":     e.Args,
			}})

		case agent.ToolResultEvent:
			if streamBuf != "" {
				s.send(wsMessage{Type: "stream_end", Data: map[string]string{"text": streamBuf}})
				streamBuf = ""
			}
			s.send(wsMessage{Type: "tool_result", Data: map[string]any{
				"toolId":   e.ToolID,
				"toolName": e.ToolName,
				"output":   e.Output,
				"isError":  e.IsError,
				"elapsed":  e.Elapsed.Seconds(),
			}})

		case agent.PermissionRequestEvent:
			id := fmt.Sprintf("perm_%d", time.Now().UnixNano())
			s.pendingPermMu.Lock()
			s.pendingPerms[id] = e.ResponseCh
			s.pendingPermMu.Unlock()
			s.send(wsMessage{Type: "permission_request", Data: map[string]string{
				"id":          id,
				"toolName":    e.ToolName,
				"description": e.Desc,
			}})

		case agent.AskUserQuestionEvent:
			id := fmt.Sprintf("ask_%d", time.Now().UnixNano())
			respCh := make(chan tools.QuestionResponse, 1)
			s.pendingAskMu.Lock()
			s.pendingAsks[id] = respCh
			s.pendingAskMu.Unlock()
			s.send(wsMessage{Type: "ask_user", Data: map[string]any{
				"id":        id,
				"questions": e.Questions,
			}})
			go func() {
				resp := <-respCh
				e.ResponseCh <- resp.Answers
			}()

		case agent.TurnComplete:
			if streamBuf != "" {
				s.send(wsMessage{Type: "stream_end", Data: map[string]string{"text": streamBuf}})
				streamBuf = ""
			}
			s.send(wsMessage{Type: "turn_complete", Data: map[string]int{"turn": e.Turn}})

		case agent.LoopComplete:
			if streamBuf != "" {
				s.send(wsMessage{Type: "stream_end", Data: map[string]string{"text": streamBuf}})
				streamBuf = ""
			}
			elapsed := time.Since(startTime).Seconds()
			s.send(wsMessage{Type: "loop_complete", Data: map[string]any{
				"totalTurns": e.TotalTurns,
				"elapsed":    elapsed,
			}})

		case agent.UsageEvent:
			s.send(wsMessage{Type: "usage", Data: map[string]int{
				"inputTokens":  e.InputTokens,
				"outputTokens": e.OutputTokens,
			}})

		case agent.ErrorEvent:
			s.send(wsMessage{Type: "error", Data: map[string]string{"message": e.Message}})

		case agent.CompactEvent:
			s.send(wsMessage{Type: "compact", Data: map[string]string{"message": e.Message}})

		case agent.RetryEvent:
			s.send(wsMessage{Type: "retry", Data: map[string]any{
				"reason": e.Reason,
				"waitMs": e.Wait.Milliseconds(),
			}})
		}
	}
}

func (s *Server) listenForAskUser(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case req, ok := <-s.askUserCh:
			if !ok {
				return
			}
			id := fmt.Sprintf("ask_%d", time.Now().UnixNano())
			respCh := make(chan tools.QuestionResponse, 1)
			s.pendingAskMu.Lock()
			s.pendingAsks[id] = respCh
			s.pendingAskMu.Unlock()
			s.send(wsMessage{Type: "ask_user", Data: map[string]any{
				"id":        id,
				"questions": req.Questions,
			}})
			resp := <-respCh
			req.ResponseCh <- resp
		}
	}
}

func (s *Server) handlePermissionResponse(data permResponseData) {
	s.pendingPermMu.Lock()
	ch, ok := s.pendingPerms[data.ID]
	if ok {
		delete(s.pendingPerms, data.ID)
	}
	s.pendingPermMu.Unlock()

	if !ok {
		return
	}

	var resp agent.PermissionResponse
	switch data.Response {
	case "allow":
		resp = agent.PermAllow
	case "deny":
		resp = agent.PermDeny
	case "allowAlways":
		resp = agent.PermAllowAlways
	default:
		resp = agent.PermDeny
	}
	reply := agent.PermissionReply{Response: resp}
	if resp == agent.PermDeny {
		reply.Feedback = data.Feedback
	}
	ch <- reply
}

func (s *Server) handleAskUserResponse(data askUserResponseData) {
	s.pendingAskMu.Lock()
	ch, ok := s.pendingAsks[data.ID]
	if ok {
		delete(s.pendingAsks, data.ID)
	}
	s.pendingAskMu.Unlock()

	if !ok {
		return
	}

	ch <- tools.QuestionResponse{Answers: data.Answers}
}

func (s *Server) send(msg wsMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.conns) == 0 {
		return
	}
	data, _ := json.Marshal(msg)
	for conn := range s.conns {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[ws] write error: type=%s err=%v", msg.Type, err)
		}
	}
}

func (s *Server) wireSkillsToAgent(wd string) {
	if s.skillCatalog == nil || s.ag == nil {
		return
	}
	s.registry.Register(&skills.LoadSkillTool{
		Catalog: s.skillCatalog,
		Host:    s,
	})
}

func (s *Server) ActivateSkill(name, body string) {
	if s.ag != nil {
		s.ag.ActivateSkill(name, body)
	}
}

func (s *Server) SetToolFilter(allow func(name string) bool) {
	if s.ag != nil {
		s.ag.SetToolFilter(allow)
	}
}

func (s *Server) ToolRegistry() *tools.Registry {
	return s.registry
}

func loadCustomInstructions(wd string) string {
	return memory.LoadInstructions(wd)
}

func buildSkillSection(catalog *skills.Catalog, wd string) string {
	if catalog == nil {
		return ""
	}
	metas := catalog.List()
	if len(metas) == 0 {
		return ""
	}
	skillsDir := filepath.Join(wd, ".yukino", "skills")
	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString(fmt.Sprintf("Skills are installed at: %s\n", skillsDir))
	sb.WriteString("When creating new skills, always place them under this directory as <skill-name>/SKILL.md.\n\n")
	for _, meta := range metas {
		desc := meta.Description
		if len(desc) > 200 {
			desc = desc[:200] + "…"
		}
		sb.WriteString(fmt.Sprintf("- /%s: %s\n", meta.Name, desc))
	}
	return sb.String()
}

func installMemExtractor(ag *agent.Agent, wd, protocol string, client llm.Client, registry *tools.Registry, conv *conversation.Manager) *extractor.Extractor {
	extr := extractor.InitExtractMemories(extractor.Deps{
		MemoryDir:     memory.GetAutoMemPath(wd),
		UserMemoryDir: memory.GetUserAutoMemPath(),
		ProjectRoot:   wd,
		Client:        client,
		ToolRegistry:  registry,
		Protocol:      protocol,
		Conversation:  conv,
		AppendSystem:  func(s string) { conv.AddSystemReminder(s) },
	})
	ag.OnLoopComplete = func(_ *conversation.Manager) {
		_ = extr.Execute(context.Background())
	}
	return extr
}

func newAgentHookRunner(client llm.Client) func(prompt string, ctx hooks.HookContext) (string, error) {
	return func(p string, _ hooks.HookContext) (string, error) {
		c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		conv := conversation.NewManager()
		conv.AddUserMessage(p)
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

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}
