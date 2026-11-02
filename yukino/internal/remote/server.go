package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/bootstrap"
	"github.com/yukino-js/yukino/yukino/internal/commands"
	"github.com/yukino-js/yukino/yukino/internal/compact"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/file_history"
	"github.com/yukino-js/yukino/yukino/internal/goal"
	"github.com/yukino-js/yukino/yukino/internal/history"
	"github.com/yukino-js/yukino/yukino/internal/hooks"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
	"github.com/yukino-js/yukino/yukino/internal/memory"
	"github.com/yukino-js/yukino/yukino/internal/memory/background"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/prompt"
	"github.com/yukino-js/yukino/yukino/internal/sandbox"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
	"github.com/yukino-js/yukino/yukino/internal/teams"
	"github.com/yukino-js/yukino/yukino/internal/todo"
	"github.com/yukino-js/yukino/yukino/internal/tools"
	"github.com/yukino-js/yukino/yukino/internal/utils"
	"github.com/yukino-js/yukino/yukino/internal/workflow"
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
	accessToken          string
	runMu                sync.Mutex
	planMu               sync.Mutex
	planApprovalID       string
	planApprovalTimer    *time.Timer
	pendingPlanExecution string
	MCPLoading           string
	providers            []config.ProviderConfig
	mcpConfigs           []config.MCPServerConfig
	hookCfgs             []config.HookConfig
	addr                 string

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
	goalManager  *goal.GoalManager

	streaming    bool
	cancelStream context.CancelFunc
	agentCh      <-chan agent.AgentEvent

	// lastUsage mirrors the reference RemoteServer.lastUsage: the most recent
	// usage event, surfaced by /status and the command context.
	lastUsageInput  int
	lastUsageOutput int

	askUserCh chan tools.AskUserRequest

	pendingPermMu sync.Mutex
	pendingPerms  map[string]chan<- agent.PermissionReply

	pendingAskMu sync.Mutex
	pendingAsks  map[string]chan tools.QuestionResponse

	cmdRegistry  *commands.Registry
	skillCatalog *skills.Catalog
	taskMgr      *subagent.TaskManager
	todoList     *todo.TaskList
	memoryMgr    *memory.Manager
	memoryTasks  *background.Tasks
	teamMgr      *teams.TeamManager
	mcpMgr       *mcp.Manager

	instructionsContent   string
	memoryContent         string
	mcpAnnounced          map[string]bool
	mcpApps               *remoteMcpApps
	enableCoordinatorMode bool
	// memoryEnabled mirrors config.yaml `enable_memory:`; gates injection,
	// extraction, and consolidation. Defaults to true.
	memoryEnabled bool
	sandboxCfg    *config.SandboxYamlConfig

	httpServer *http.Server
}

func NewServer(providers []config.ProviderConfig, mcpConfigs []config.MCPServerConfig, hookCfgs []config.HookConfig, addr string, enableCoordinatorMode bool, defaultProvider int, sandboxCfg *config.SandboxYamlConfig, memoryEnabled bool) *Server {
	return &Server{
		accessToken:           createAccessToken(),
		providers:             providers,
		mcpConfigs:            mcpConfigs,
		hookCfgs:              hookCfgs,
		addr:                  addr,
		defaultProvider:       defaultProvider,
		enableCoordinatorMode: enableCoordinatorMode,
		memoryEnabled:         memoryEnabled,
		sandboxCfg:            sandboxCfg,
		conns:                 make(map[*websocket.Conn]struct{}),
		pendingPerms:          make(map[string]chan<- agent.PermissionReply),
		pendingAsks:           make(map[string]chan tools.QuestionResponse),
		mcpAnnounced:          map[string]bool{},
		mcpApps:               newRemoteMcpApps(),
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

	address, err := s.listenAddress()
	if err != nil {
		return err
	}
	fmt.Printf("Remote server listening at http://%s/#token=%s\n", address, s.accessToken)
	s.httpServer = &http.Server{Addr: address, Handler: mux}
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop tears the server down: closes the HTTP/WS listener and disposes the
// tool registry (MCP children, background tasks). Run returns nil once the
// server has stopped.
func (s *Server) mcpAppHandle() mcpAppHandle {
	handle := mcpAppHandle{SessionID: s.sessionID, Cwd: mustGetwd(), MCPManager: s.mcpMgr}
	if s.ag != nil {
		handle.Checker = s.ag.Checker
		handle.Hooks = s.ag.Hooks
		handle.ToolFilter = s.ag.ToolNameFilter
	}
	return handle
}

func (s *Server) handleMcpAppRequest(ws *websocket.Conn, request mcp.McpAppRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), PendingRequestTimeoutMS*time.Millisecond)
	defer cancel()
	if s.mcpApps == nil {
		s.sendTo(ws, wsMessage{Type: "mcp_app_response", Data: map[string]any{"requestId": request.RequestID, "error": MCPAppAgentDisconnected}})
		return
	}
	result, err := s.mcpApps.Request(ctx, request, s.mcpAppHandle(), s.requestPermissionViaWS)
	if err != nil {
		s.sendTo(ws, wsMessage{Type: "mcp_app_response", Data: map[string]any{"requestId": request.RequestID, "error": err.Error()}})
		return
	}
	s.sendTo(ws, wsMessage{Type: "mcp_app_response", Data: map[string]any{"requestId": request.RequestID, "result": result}})
}

func (s *Server) sendTo(ws *websocket.Conn, msg wsMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_ = ws.WriteMessage(websocket.TextMessage, data)
}

func (s *Server) Stop() {
	if s.httpServer != nil {
		_ = s.httpServer.Close()
	}
	if s.memoryTasks != nil {
		s.memoryTasks.Dispose()
	}
	if s.mcpApps != nil {
		s.mcpApps.Clear()
	}
	if s.registry != nil {
		_ = s.registry.Dispose()
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if status, message := authorizeWebSocket(r, "/ws", s.accessToken); status != 0 {
		http.Error(w, message, status)
		return
	}
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

		case "mcp_app_request":
			var data struct {
				RequestID string         `json:"requestId"`
				AppID     string         `json:"appId"`
				Method    string         `json:"method"`
				Params    map[string]any `json:"params"`
			}
			if json.Unmarshal(msg.Data, &data) == nil {
				request := mcp.McpAppRequest{RequestID: data.RequestID, AppID: data.AppID, Method: data.Method, Params: data.Params}
				if mcp.ValidMcpAppRequest(request) {
					go s.handleMcpAppRequest(conn, request)
				}
			}

		case "ask_user_response":
			var data askUserResponseData
			json.Unmarshal(msg.Data, &data)
			s.handleAskUserResponse(data)

		case "plan_approval_response":
			var data struct {
				ID       string `json:"id"`
				Choice   string `json:"choice"`
				Feedback string `json:"feedback"`
			}
			if json.Unmarshal(msg.Data, &data) == nil {
				go s.handlePlanApprovalResponse(data.ID, data.Choice, data.Feedback)
			}
		case "cancel":
			s.runMu.Lock()
			cancel := s.cancelStream
			s.runMu.Unlock()
			if cancel != nil {
				cancel()
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
	s.cmdRegistry = commands.CreateDefaultRegistry()
	s.skillCatalog = skills.LoadCatalog(wd)
	s.instructionsContent = loadCustomInstructions(wd)
	s.memoryContent = ""

	skillSection := skills.BuildSkillSection(s.skillCatalog)

	env := prompt.DetectEnvironment(wd)
	env.Model = p.Model
	systemPrompt := prompt.BuildSystemPrompt(env)

	client, err := llm.NewClient(p, systemPrompt)
	if err != nil {
		return err
	}
	s.client = client
	s.conv = conversation.NewManager()
	s.sessionID = session.NewID()
	s.fileHistory = file_history.New(s.sessionID)

	s.registerTools(client, p, wd)
	if service := workflow.GetWorkflowService(s.registry); service != nil {
		_ = service.RegisterNamedCommands(s.cmdRegistry)
	}

	ag := agent.New(client, s.registry, p.Protocol)
	ag.ContextWindow = config.GetContextWindow(p)
	ag.MaxOutputTokens = config.GetMaxOutputTokens(p)
	ag.Instructions = s.instructionsContent
	ag.MemoryContent = s.memoryContent
	ag.SkillSection = skillSection
	ag.FileHistory = s.fileHistory
	ag.SetSessionID(s.sessionID)
	ag.GoalManager = s.goal()

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
	s.teamMgr.SetPermissionChecker(ag.Checker)

	if at, ok := s.registry.Get("Agent").(*subagent.AgentTool); ok {
		at.ParentChecker = ag.Checker
	}

	s.wireSkillsToAgent(wd)
	if s.memoryEnabled {
		s.memoryTasks = installMemoryTasks(ag, wd, client, s.conv)
	}

	return nil
}

func (s *Server) registerTools(client llm.Client, p *config.ProviderConfig, wd string) {
	s.taskMgr = subagent.NewTaskManager()
	if err := s.taskMgr.UseSession(s.sessionID); err != nil {
		panic(err)
	}
	store, err := todo.NewStore(s.sessionID)
	if err != nil {
		panic(err)
	}
	s.todoList, err = todo.NewTaskList(store)
	if err != nil {
		panic(err)
	}
	// enable_memory: false keeps the index out of the conversation and nothing
	// is injected, extracted, or consolidated automatically.
	if s.memoryEnabled {
		s.memoryMgr = memory.NewManager(wd, nil)
		s.memoryContent = s.memoryMgr.BuildSystemReminder()
	}
	loader := subagent.NewAgentLoader()
	loader.LoadAll()
	s.teamMgr = teams.NewTeamManager(wd)
	s.teamMgr.CoordinatorMode = s.enableCoordinatorMode

	s.defaultTools = bootstrap.CreateToolRegistry(s.todoList, bootstrap.ToolRegistryOptions{
		InteractionMode: "interactive",
		TeamMgr:         s.teamMgr,
		WorkDir:         wd,
		GoalManager:     func() *goal.GoalManager { return s.goal() },
	})
	s.registry = s.defaultTools.Registry
	s.registry.Register(&tools.AskUserQuestionTool{RequestCh: s.askUserCh})
	bootstrap.ConfigureBashSandbox(s.registry, wd, s.sandboxCfg, nil)
	s.defaultTools.EditFile.FileHistory = s.fileHistory
	s.defaultTools.WriteFile.FileHistory = s.fileHistory
	tools.AttachBackgroundTaskManager(s.registry, s.taskMgr)
	manager := s.taskMgr
	s.registry.AddCleanup(func() error { return manager.StopAll(context.Background()) })
	s.registry.Register(&teams.TeamCreateTool{TeamMgr: s.teamMgr})
	s.registry.Register(&teams.TeamDeleteTool{TeamMgr: s.teamMgr})
	s.registry.Register(&teams.TeamListTool{TeamMgr: s.teamMgr})
	s.registry.Register(&teams.SendMessageTool{TeamMgr: s.teamMgr, SenderName: "leader"})
	s.registry.Register(&teams.TaskStopTool{TeamMgr: s.teamMgr, TaskManager: s.taskMgr})
	s.registry.Register(&tools.SyntheticOutputTool{})
	s.registry.Register(&subagent.AgentTool{
		Client:        client,
		Provider:      p,
		MemoryContent: s.memoryContent,
		Registry:      s.registry,
		Protocol:      p.Protocol,
		TaskMgr:       s.taskMgr,
		Loader:        loader,
		Conversation:  s.conv,
		TeamMgr:       s.teamMgr,
	})
	workflow.AttachWorkflow(workflow.WorkflowServiceOptions{
		WorkflowBackendOptions: workflow.WorkflowBackendOptions{
			Registry:    s.registry,
			GetClient:   func() llm.Client { return s.client },
			GetProvider: func() *config.ProviderConfig { return s.activeProvider() },
		},
		Cwd:         wd,
		TaskManager: s.taskMgr,
	})
}

func (s *Server) initMCPServers() {
	if len(s.mcpConfigs) == 0 {
		return
	}
	mgr := mcp.NewManager(true)
	result := mgr.ConnectAll(context.Background(), s.mcpConfigs)
	s.mcpMgr = mgr
	for _, connected := range result.Tools {
		s.registry.Register(connected.Tool)
	}
	for _, connectError := range result.Errors {
		log.Printf("MCP error: %s: %s", connectError.ServerName, connectError.Error)
	}
	if len(s.providers) > 0 {
		p := s.activeProvider()
		mcp.DecideAndApply(s.registry, p.BaseURL, p.Protocol, config.GetContextWindow(p), s.MCPLoading)
	}
}

func (s *Server) handleUserMessage(content string) {

	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	if _, _, isCommand := commands.Parse(content); isCommand {
		s.handleSlashCommand(content)
		return
	}

	ctx, ok := s.beginRun()
	if !ok {
		return
	}
	defer s.finishRun()
	wd, _ := os.Getwd()
	input, err := history.RecordInput(history.InputOptions{Cwd: wd, SessionID: &s.sessionID, Source: "ws", Text: content})
	if err != nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
		return
	}
	expanded, blocks := conversation.ExpandAtRefsWithImages(content, wd)
	userRecord := session.Message{Content: expanded}
	if blocks != nil {
		userRecord = session.Message{ContentBlocks: blocks}
	}
	recorded, err := session.SaveUserMessage(wd, s.sessionID, input, userRecord)
	if err != nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
		return
	}
	s.conv.AppendMessages([]conversation.Message{recorded.ToConversation()})

	if s.mcpMgr != nil {
		mcp.SyncMcpInstructions(s.conv, s.mcpAnnounced, s.mcpMgr)
	}

	s.agentCh = s.ag.Run(ctx, s.conv)

	askDone := make(chan struct{})
	go s.listenForAskUser(askDone)
	s.consumeAgentEvents()
	close(askDone)

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

	wd, _ := os.Getwd()
	entry, err := history.RecordInput(history.InputOptions{Cwd: wd, SessionID: &s.sessionID, Source: "ws", Text: input, CommandName: &name})
	if err != nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
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
	case "workflows":
		service := workflow.GetWorkflowService(s.registry)
		message := "Workflow service is not initialized"
		if service != nil {
			result, err := service.Command(args, s.workflowHostContext(wd))
			if err != nil {
				s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
				s.send(wsMessage{Type: "command_done", Data: nil})
				return
			}
			message = result
		}
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": message}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	case "status":
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": s.buildSessionStatus()}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	case "memory":
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": s.buildMemoryStatus(args)}})
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
	case "goal":
		result := goal.HandleGoalCommand(s.goal(), args)
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": result.Message}})
		if result.Prompt == "" {
			s.send(wsMessage{Type: "command_done", Data: nil})
			return
		}
		c, ok := s.beginRun()
		if !ok {
			return
		}
		defer s.finishRun()
		recorded, err := session.SaveUserMessage(wd, s.sessionID, entry, session.Message{Content: result.Prompt})
		if err != nil {
			s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
			return
		}
		s.conv.AppendMessages([]conversation.Message{recorded.ToConversation()})
		if s.mcpMgr != nil {
			mcp.SyncMcpInstructions(s.conv, s.mcpAnnounced, s.mcpMgr)
		}
		s.ag.GoalManager = s.goal()
		s.agentCh = s.ag.Run(c, s.conv)
		askDone := make(chan struct{})
		go s.listenForAskUser(askDone)
		s.consumeAgentEvents()
		close(askDone)
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
			s.conv.Reset()
			if s.ag != nil {
				s.ag.ClearActiveSkills()
				s.ag.SetToolFilter(teams.CoordinatorToolFilter(s.enableCoordinatorMode))
			}
			s.send(wsMessage{Type: "clear", Data: nil})

		case "compact":
			s.handleCompact(args)
			return

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
		c, ok := s.beginRun()
		if !ok {
			return
		}
		defer s.finishRun()
		wd, _ := os.Getwd()
		if cmd.Agent == "" {
			recorded, err := session.SaveUserMessage(wd, s.sessionID, entry, session.Message{Content: prompt})
			if err != nil {
				s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
				return
			}
			s.conv.AppendMessages([]conversation.Message{recorded.ToConversation()})
		}

		if s.mcpMgr != nil {
			mcp.SyncMcpInstructions(s.conv, s.mcpAnnounced, s.mcpMgr)
		}

		if cmd.Agent != "" {
			s.agentCh = commands.RunSubagentCommand(c, cmd.Agent, prompt, displayText, s.registry, s.ag.Checker, s.conv, wd, s.sessionID, &entry)
		} else {
			s.agentCh = s.ag.Run(c, s.conv)
		}

		askDone := make(chan struct{})
		go s.listenForAskUser(askDone)
		s.consumeAgentEvents()
		close(askDone)

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
		TokenCount: func() (int, int) { return s.lastUsageInput, s.lastUsageOutput },
		PermissionMode: func() string {
			if s.ag != nil && s.ag.Checker != nil {
				return string(s.ag.Checker.Mode())
			}
			return "default"
		},
		ToolCount: func() int { return len(s.registry.ListTools()) },
		MemoryList: func() []string {
			if s.memoryMgr == nil || !s.memoryEnabled {
				return nil
			}
			memories := s.memoryMgr.GetMemories()
			names := make([]string, 0, len(memories))
			for _, memory := range memories {
				names = append(names, memory.Name)
			}
			return names
		},
	}
}

func (s *Server) handleCompact(customInstructions string) {
	if s.client == nil || s.conv == nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{"message": "Compact requires an active provider."}})
		s.send(wsMessage{Type: "command_done", Data: nil})
		return
	}
	s.send(wsMessage{Type: "system", Data: map[string]string{"message": "Compacting conversation…"}})
	wd, _ := os.Getwd()
	var recovery *compact.RecoveryState
	var schemas []map[string]any
	if s.ag != nil {
		recovery = s.ag.RecoveryState
		schemas = s.ag.Registry.GetAllSchemas(s.ag.Protocol)
	}
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		if name := tools.SchemaName(schema); name != "" {
			names = append(names, name)
		}
	}
	sessionFilePath := session.SessionFilePath(wd, s.sessionID)
	result, err := compact.ForceCompact(context.Background(), s.conv, s.client, recovery, names, schemas, sessionFilePath, customInstructions)
	if err != nil {
		s.send(wsMessage{Type: "error", Data: map[string]string{"message": err.Error()}})
	} else {
		if result.Boundary != nil {
			_ = session.SaveCompactBoundary(wd, s.sessionID, result.Boundary.Summary, result.Boundary.Keep)
		}
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": "⟳ Compacted: " + result.Message}})
	}
	s.send(wsMessage{Type: "command_done", Data: nil})
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

	msgs := session.LoadSessionForResume(wd, targetID)
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
	s.conv.Reset()
	s.sessionID = targetID
	if s.ag != nil {
		s.ag.SetSessionID(s.sessionID)
		s.ag.GoalManager = s.goal()
	}

	boundary, after, compacted := session.FindLastCompactBoundary(msgs)
	replay := session.RebuildFromSession(msgs, false)

	s.send(wsMessage{Type: "clear", Data: nil})
	for _, msg := range replay {
		s.conv.AppendMessages([]conversation.Message{msg})
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

var agentEventLogger = AgentEventLogger{}

func (s *Server) consumeAgentEvents() {
	streamBuf := ""
	startTime := time.Now()

	for ev := range s.agentCh {
		agentEventLogger.OnEvent(ev)
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
			data := map[string]any{
				"toolId":   e.ToolID,
				"toolName": e.ToolName,
				"output":   e.Output,
				"isError":  e.IsError,
				"elapsed":  e.Elapsed.Seconds(),
			}
			if e.McpApp != nil {
				data["mcpApp"] = e.McpApp
				s.mcpApps.Register(mcp.McpAppToolResult{
					ToolID:   e.ToolID,
					ToolName: e.ToolName,
					Output:   e.Output,
					IsError:  e.IsError,
					Elapsed:  e.Elapsed.Seconds(),
					McpApp: &mcp.McpApp{
						ServerName:  e.McpApp.ServerName,
						ToolName:    e.McpApp.ToolName,
						ResourceURI: e.McpApp.ResourceURI,
						Arguments:   e.McpApp.Arguments,
						Result:      e.McpApp.Result,
					},
				}, s.mcpAppHandle())
			}
			s.send(wsMessage{Type: "tool_result", Data: data})

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
			if e.PlanReady {
				s.requestPlanApproval()
			}
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
			s.lastUsageInput = e.InputTokens
			s.lastUsageOutput = e.OutputTokens
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
	if msg.Type == "clear" && s.mcpApps != nil {
		s.mcpApps.Clear()
	}
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
	s.wireSkillCommands()
	s.registry.Register(&skills.LoadSkillTool{
		Catalog:  s.skillCatalog,
		Host:     s,
		ForkHost: s,
	})
	s.registry.Register(&skills.InstallSkillTool{
		Cwd:         wd,
		Catalog:     s.skillCatalog,
		OnInstalled: s.wireSkillCommands,
	})
}

func (s *Server) wireSkillCommands() {
	for _, meta := range s.skillCatalog.List() {
		if s.cmdRegistry.Find(meta.Name) != nil {
			continue
		}
		command := &commands.Command{Name: meta.Name, Description: meta.Description + " [skill]", IsSkill: true}
		if meta.IsFork() {
			command.Type = commands.TypeSkillFork
			command.Handler = func(*commands.Context) string { return "" }
		} else {
			captured := meta.Name
			command.Type = commands.TypePrompt
			command.Handler = func(ctx *commands.Context) string {
				skill := s.skillCatalog.Get(captured)
				if skill == nil {
					return fmt.Sprintf("[skill error] unknown skill: %s", captured)
				}
				return skills.RunInline(skill, ctx.Args, s)
			}
		}
		s.cmdRegistry.Register(command)
	}
}

func (s *Server) ActivateSkill(name, body string) {
	if s.ag != nil {
		s.ag.ActivateSkill(name, body)
	}
}

func (s *Server) SnapshotParentMessages(count int) string {
	messages := s.conv.GetMessages()
	if len(messages) > count {
		messages = messages[len(messages)-count:]
	}
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		content := any(message.Content)
		if len(message.ContentBlocks) > 0 {
			content = message.ContentBlocks
		}
		lines = append(lines, message.Role+": "+utils.ContentToText(content))
	}
	return strings.Join(lines, "\n")
}

func (s *Server) RunSubagent(ctx context.Context, prompt string) (string, error) {
	if s.client == nil || s.ag == nil {
		return "", errors.New("RunSubagent: agent is not ready")
	}
	spec := subagent.BuiltinSpecs["general-purpose"]
	definition := &subagent.AgentDefinition{
		AgentType:       spec.Name,
		WhenToUse:       spec.Description,
		Tools:           spec.Tools,
		DisallowedTools: spec.DisallowedTools,
		SystemPrompt:    spec.SystemPromptOverride,
		MaxTurns:        spec.MaxTurns,
		Model:           spec.Model,
		PermissionMode:  spec.PermissionMode,
		ReadOnly:        spec.ReadOnly,
		Background:      spec.Background,
		Worktree:        spec.Worktree,
		InitialPrompt:   spec.InitialPrompt,
	}
	wd, _ := os.Getwd()
	return subagent.SpawnSubagent(ctx, definition, prompt, s.client, s.registry, s.activeProvider(), wd, nil, nil, "", s.ag.Checker, subagent.SubagentRunOptions{Background: false})
}

func (s *Server) SetToolFilter(allow func(name string) bool) {
	if s.ag != nil {
		s.ag.SetToolFilter(allow)
	}
}

func (s *Server) ToolRegistry() *tools.Registry {
	return s.registry
}

// workflowHostContext builds the tool execution context carried into
// workflow agent runs: the permission checker plus a WS round-trip
// permission callback, mirroring createPermissionCallback.
func (s *Server) workflowHostContext(wd string) context.Context {
	execution := tools.ExecutionContext{
		WorkDir:           wd,
		SessionID:         s.sessionID,
		RequestPermission: s.requestPermissionViaWS,
	}
	if s.ag != nil && s.ag.Checker != nil {
		execution.Plan = &tools.PlanContext{Owner: s.ag.Checker}
	}
	return tools.WithExecutionContext(context.Background(), execution)
}

func (s *Server) requestPermissionViaWS(ctx context.Context, toolName, desc string) (int, string) {
	id := fmt.Sprintf("perm_%d", time.Now().UnixNano())
	responseCh := make(chan agent.PermissionReply, 1)
	s.pendingPermMu.Lock()
	s.pendingPerms[id] = responseCh
	s.pendingPermMu.Unlock()
	s.send(wsMessage{Type: "permission_request", Data: map[string]string{
		"id":          id,
		"toolName":    toolName,
		"description": desc,
	}})
	select {
	case reply := <-responseCh:
		return int(reply.Response), reply.Feedback
	case <-ctx.Done():
		s.pendingPermMu.Lock()
		delete(s.pendingPerms, id)
		s.pendingPermMu.Unlock()
		return int(agent.PermDeny), ""
	}
}

func loadCustomInstructions(wd string) string {
	return memory.LoadInstructions(wd)
}

func installMemoryTasks(ag *agent.Agent, wd string, client llm.Client, conv *conversation.Manager) *background.Tasks {
	tasks := background.New(client, wd, background.Callbacks{
		AppendSystem: func(s string) { conv.AddSystemReminder(s) },
	})
	ag.OnLoopComplete = func(loopConv *conversation.Manager) {
		tasks.Run(loopConv)
	}
	return tasks
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

// buildSessionStatus renders the rich /status block (src/remote/server.ts
// buildSessionStatus parity: provider, sandbox, MCP, session).
func (s *Server) buildSessionStatus() string {
	if s.ag == nil {
		return "Agent is not ready yet."
	}
	p := s.activeProvider()
	var model, providerName, protocol string
	if p != nil {
		model = p.Model
		providerName = p.Name
		protocol = p.Protocol
	}
	permMode := string(s.ag.Checker.Mode())
	sbStatus := "OFF"
	if s.ag.Checker.SandboxEnabled {
		sb := sandbox.New()
		state := "blocked"
		if sb != nil && sb.Available() {
			state = "ready"
		}
		label := "ON (manual)"
		if s.ag.Checker.SandboxAutoAllow {
			label = "ON (auto-allow)"
		}
		sbStatus = label + ", " + state
	}
	memories := "disabled (enable_memory: false)"
	if s.memoryEnabled {
		count := 0
		if s.memoryMgr != nil {
			count = len(s.memoryMgr.GetMemories())
		}
		memories = strconv.Itoa(count)
	}
	skillCount := 0
	if s.skillCatalog != nil {
		skillCount = len(s.skillCatalog.List())
	}
	mcpServers := 0
	if s.mcpMgr != nil {
		mcpServers = len(s.mcpMgr.ConnectedServers())
	}
	wd, _ := os.Getwd()
	lines := []string{
		"Mode:      " + permMode,
		"Model:     " + model,
		fmt.Sprintf("Provider:  %s (%s)", providerName, protocol),
		fmt.Sprintf("Tokens:    %d in / %d out", s.lastUsageInput, s.lastUsageOutput),
		fmt.Sprintf("Tools:     %d", len(s.registry.ListTools())),
		"Sandbox:   " + sbStatus,
		"Memories:  " + memories,
		fmt.Sprintf("Skills:    %d", skillCount),
		fmt.Sprintf("MCP:       %d server(s), %d tool(s)", mcpServers, bootstrap.CountMcpTools(s.registry)),
		"Session:   " + s.sessionID,
		"Directory: " + wd,
	}
	return strings.Join(lines, "\n")
}

// buildMemoryStatus renders /memory output (src/remote/server.ts
// buildMemoryStatus parity).
func (s *Server) buildMemoryStatus(args string) string {
	if s.ag == nil {
		return "Memory is not available yet."
	}
	if !s.memoryEnabled {
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
