package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/commands"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/file_history"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
	"github.com/yukino-js/yukino/yukino/internal/memory"
	"github.com/yukino-js/yukino/yukino/internal/memory/consolidation"
	"github.com/yukino-js/yukino/yukino/internal/memory/extractor"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
	"github.com/yukino-js/yukino/yukino/internal/teams"
	"github.com/yukino-js/yukino/yukino/internal/todo"
	"github.com/yukino-js/yukino/yukino/internal/tools"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type appState int

const (
	stateProviderSelect appState = iota
	stateChat
)

type Model struct {
	providers        []config.ProviderConfig
	selectedProvider *config.ProviderConfig
	defaultProvider  int
	client           llm.Client
	registry         *tools.Registry
	ag               *agent.Agent

	state     appState
	streaming bool

	textarea textarea.Model
	viewport viewport.Model
	width    int
	height   int
	ready    bool

	providerCursor int

	conversation *conversation.Manager

	chatMessages []chatMessage
	toolBlocks   []toolBlockInfo
	streamBuf    string
	agentCh      <-chan agent.AgentEvent
	cancelStream context.CancelFunc

	totalInput  int
	totalOutput int

	permDialog   bool
	permToolName string
	permDesc     string
	permRespCh   chan<- agent.PermissionReply
	permCursor   int
	permFeedback string

	cmdRegistry   *commands.Registry
	slashMenuOpen bool
	slashMatches  []*commands.Command
	slashCursor   int

	userScrolled  bool
	committedUpTo int

	atMenuOpen bool
	atMatches  []string
	atCursor   int
	atPrefix   string

	spinner       spinner.Model
	thinking      bool
	thinkingStart time.Time
	thinkingDone  float64

	instructionsContent string
	memoryContent       string

	mcpConfigs        []config.MCPServerConfig
	mcpMgr            *mcp.Manager
	mcpConnecting     bool
	mcpInstructions   string
	mcpInstructionsOK bool
	mcpServerInfo     string
	hookConfigs       []config.HookConfig

	historyEntries []string
	historyIndex   int
	historyDraft   string

	sessionID    string
	fileHistory  *file_history.History
	defaultTools tools.DefaultTools

	planApprovalDialog   bool
	planApprovalCursor   int
	planApprovalInput    string
	planFeedbackRequired bool

	rewindDialog       bool
	rewindSnapshots    []file_history.Snapshot
	rewindCursor       int
	rewindPhase        int
	rewindOptionCursor int

	askUserCh          chan tools.AskUserRequest
	subAgentProgressCh chan subagent.SubAgentProgress
	activeSubAgent     *subAgentBlock
	askUserDialog      bool
	askUserQuestions   []tools.Question
	askUserCursors     []int
	askUserSelected    []map[int]bool
	askUserOther       []string
	askUserQIdx        int
	askUserRespCh      chan tools.QuestionResponse
	askUserAnswered    map[int]string
	askUserOnSubmit    bool
	askUserSubmitIdx   int
	skillCatalog       *skills.Catalog
	announcedSkills    map[string]bool
	taskMgr            *subagent.TaskManager
	todoList           *todo.TaskList
	memoryMgr          *memory.Manager
	memoryExtractor    *extractor.Extractor
	memoryConsolidator *consolidation.Consolidator
	teamMgr            *teams.TeamManager

	sandboxDialog         bool
	sandboxCursor         int
	sandboxCfg            *config.SandboxYamlConfig
	EnableCoordinatorMode bool
	MCPLoading            string
	ForkDisabled          bool

	modelDialog bool
	modelState  modelPickerState
	modelQuery  string
	// modelFocusedID tracks focus by model id (the entry identity), matching
	// the reference focusedId; modelFocusOther marks the free-text row.
	modelFocusedID  string
	modelFocusOther bool

	modelCustomMode   bool
	modelCustomText   string
	modelCustomCursor int

	loginDialog      bool
	loginInitial     *config.ProviderConfig
	loginForm        providerLoginForm
	loginCursor      int
	loginFieldCursor int
	loginFieldErrors map[string]string
	loginFormError   string
	loginSubmitting  bool
	// loginModelEdited blocks the discovery auto-fill once the user typed in
	// the model field.
	loginModelEdited   bool
	loginDiscovery     loginDiscoveryState
	loginDiscoveryGen  int
	loginCompletedConn *loginConnection

	providerDialog   bool
	providerQuery    string
	providerFocusURL string
	providerFocusSet bool

	resumeSessions []session.SessionInfo
	resumeDialog   bool
	resumeQuery    string
	// resumeFocusedID tracks focus by session id, matching the reference
	// focusedId.
	resumeFocusedID string
	resumeFocusSet  bool
}

// dialogAvailableRows is the row budget for a docked dialog: the terminal
// height minus the separator, input and status bar.
func (m Model) dialogAvailableRows() int {
	rows := m.height - m.textarea.Height() - 4
	if rows < 0 {
		rows = 0
	}
	return rows
}

func New(providers []config.ProviderConfig, mcpConfigs []config.MCPServerConfig, hookConfigs []config.HookConfig, sandboxCfg *config.SandboxYamlConfig, defaultProvider int) Model {
	ta := textarea.New()
	ta.Placeholder = "Send a message..."
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.SetHeight(1)

	sp := spinner.New()
	sp.Spinner = spinner.Spinner{
		Frames: SpinnerFrames,
		FPS:    80 * time.Millisecond,
	}
	sp.Style = lipgloss.NewStyle().Foreground(brandPurple)

	askCh := make(chan tools.AskUserRequest, 1)
	subProgressCh := make(chan subagent.SubAgentProgress, 32)
	dt := tools.CreateDefaultTools()
	reg := dt.Registry
	reg.Register(&tools.AskUserQuestionTool{RequestCh: askCh})

	sCfg := sandboxCfg
	initialTodos, _ := todo.NewTaskList(nil)

	m := Model{
		providers:          providers,
		todoList:           initialTodos,
		mcpConfigs:         mcpConfigs,
		hookConfigs:        hookConfigs,
		sandboxCfg:         sCfg,
		state:              stateProviderSelect,
		defaultProvider:    defaultProvider,
		providerCursor:     defaultProvider,
		textarea:           ta,
		conversation:       conversation.NewManager(),
		registry:           reg,
		defaultTools:       dt,
		cmdRegistry:        newUICommandRegistry(),
		spinner:            sp,
		askUserCh:          askCh,
		subAgentProgressCh: subProgressCh,
	}
	if m.providerCursor < 0 || m.providerCursor >= len(providers) {
		m.providerCursor = 0
	}

	if len(providers) > 0 {
		m.state = stateChat
		// The transcript header renders on the first WindowSizeMsg, before the
		// init command lands, so the remembered provider must be resolved here
		// (the reference initializes selectedProvider in useState).
		m.selectedProvider = config.ResolveDefaultProvider(providers, defaultProvider)
	}

	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink}
	if len(m.providers) > 0 {
		cmds = append(cmds, func() tea.Msg { return initSingleProviderMsg{} })
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(msg.Width - 4)

		statusHeight := 1
		sepHeight := 2
		inputHeight := m.textarea.Height() + 1
		vpHeight := msg.Height - statusHeight - sepHeight - inputHeight - 1
		if vpHeight < 1 {
			vpHeight = 1
		}

		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.viewport.MouseWheelEnabled = false
			m.ready = true
			m.updateViewport()
			return m, tea.Println(m.renderTranscriptHeader())
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		m.updateViewport()
		return m, nil

	case subAgentProgressMsg:
		p := msg.progress
		if p.Done {
			if m.activeSubAgent != nil {
				m.activeSubAgent.done = true
				m.activeSubAgent.toolCount = p.ToolCount
				m.activeSubAgent.totalTime = p.TotalTime
			}
		} else {
			if m.activeSubAgent == nil || m.activeSubAgent.done {
				m.activeSubAgent = &subAgentBlock{
					desc:      p.AgentDesc,
					agentType: p.AgentType,
				}
			}
			m.activeSubAgent.toolUses = append(m.activeSubAgent.toolUses, toolBlockInfo{
				toolName: p.ToolName,
				args:     p.ToolArgs,
				elapsed:  p.Elapsed,
				isError:  p.IsError,
			})
		}
		m.updateViewport()
		return m, m.listenForSubAgentProgress()

	case askUserMsg:
		m.askUserDialog = true
		m.askUserQuestions = msg.req.Questions
		m.askUserRespCh = msg.req.ResponseCh
		m.askUserQIdx = 0
		m.askUserCursors = make([]int, len(msg.req.Questions))
		m.askUserSelected = make([]map[int]bool, len(msg.req.Questions))
		m.askUserOther = make([]string, len(msg.req.Questions))
		m.askUserAnswered = make(map[int]string)
		m.askUserOnSubmit = false
		m.askUserSubmitIdx = 0
		for i := range msg.req.Questions {
			m.askUserSelected[i] = make(map[int]bool)
		}
		m.updateViewport()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.streaming && m.cancelStream != nil {
				m.interruptForeground()
				return m, nil
			}

			if m.cancelStream != nil {
				m.cancelStream()
			}
			if m.mcpMgr != nil {
				m.mcpMgr.Shutdown()
			}
			if m.memoryExtractor != nil {
				_ = m.memoryExtractor.Drain(5000)
			}
			return m, tea.Quit
		}

		if m.permDialog {
			return m.handlePermDialog(msg)
		}

		switch m.state {
		case stateProviderSelect:
			return m.handleProviderSelect(msg)
		case stateChat:
			return m.handleChat(msg)
		}

	case loginDiscoveryTickMsg:
		return m.handleLoginDiscoveryTick(msg)

	case loginDiscoveryDoneMsg:
		return m.handleLoginDiscoveryDone(msg)

	case initSingleProviderMsg:
		p := config.ResolveDefaultProvider(m.providers, m.defaultProvider)
		m.selectedProvider = p
		return m, m.activateInitialProvider(p)

	case forkSkillDoneMsg:
		var commit string
		if msg.err != nil {
			line := fmt.Sprintf("Skill %s (fork) failed: %v", msg.name, msg.err)
			m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: line})
			commit = errorStyle.Render("✖ " + line)
		} else {
			result := strings.TrimSpace(msg.result)
			if result == "" {
				result = fmt.Sprintf("(Skill %s returned no output)", msg.name)
			}
			m.chatMessages = append(m.chatMessages, chatMessage{role: "assistant", content: result})
			commit = m.renderMessagesRange(len(m.chatMessages)-1, len(m.chatMessages))
		}
		m.committedUpTo = len(m.chatMessages)
		m.updateViewport()
		if commit != "" {
			return m, tea.Println(commit)
		}
		return m, nil

	case compactDoneMsg:
		switch {
		case msg.err != nil:
			m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: "Compact failed: " + msg.err.Error()})
		case msg.message == "":
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "Compact: no changes."})
		default:
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "Compact: " + msg.message})
		}
		m.updateViewport()
		return m, nil

	case modelDiscoveryDoneMsg:
		if msg.err != nil {
			m.modelState = modelPickerState{status: ModelPickerError}
		} else if len(msg.models) == 0 {
			m.modelState = modelPickerState{status: ModelPickerEmpty}
		} else {
			m.modelState = modelPickerState{status: ModelPickerReady, models: msg.models}
		}
		m.updateViewport()
		return m, nil

	case mcpReadyMsg:
		m.mcpConnecting = false
		m.mcpMgr = msg.result.Mgr
		for _, t := range msg.result.Tools {
			m.registry.Register(t)
		}
		var mcpPrintLines []string
		for _, errMsg := range msg.result.Errors {
			m.chatMessages = append(m.chatMessages, chatMessage{
				role:    "error",
				content: errMsg,
			})
			mcpPrintLines = append(mcpPrintLines, errorStyle.Render("✖ "+errMsg))
		}
		if p := m.selectedProvider; p != nil {
			mcp.DecideAndApply(m.registry, p.BaseURL, p.Protocol, config.GetContextWindow(p), m.MCPLoading)
		}
		registered := len(msg.result.Tools)
		if registered > 0 {
			m.mcpServerInfo = fmt.Sprintf("Connected to %d MCP server(s), %d tools registered", len(m.mcpConfigs)-len(msg.result.Errors), registered)
		}
		m.committedUpTo = len(m.chatMessages)
		if len(msg.result.Servers) > 0 {
			toolsByServer := make(map[string][]string)
			for _, t := range msg.result.Tools {
				toolName := t.Name()
				for _, srv := range msg.result.Servers {
					if strings.HasPrefix(toolName, mcp.MCPToolNamePrefix(srv.Name)) {
						toolsByServer[srv.Name] = append(toolsByServer[srv.Name], toolName)
						break
					}
				}
			}

			var mcpParts []string
			for _, srv := range msg.result.Servers {
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
			m.mcpInstructions = "# MCP Server Instructions\n\nThe following MCP servers are connected. Use their tools when the user asks.\n\n" + strings.Join(mcpParts, "\n\n")
		}
		m.updateViewport()
		if len(mcpPrintLines) > 0 {
			return m, tea.Println(strings.Join(mcpPrintLines, "\n"))
		}
		return m, nil

	case spinner.TickMsg:
		if m.streaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.updateViewport()
			return m, cmd
		}

	case agentReadyMsg:
		m.agentCh = msg.ch
		return m, m.listenForAgentEvents()

	case agentEventMsg:
		if msg.ch != m.agentCh {
			return m, nil
		}
		return m.handleAgentEvent(msg.event)

	case agentDoneMsg:
		if msg.ch != m.agentCh {
			return m, nil
		}
		m.finishStreaming()
		return m, nil

	case agentErrMsg:
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "error",
			content: msg.err.Error(),
		})
		m.finishStreaming()
		return m, nil

	case mailboxPollMsg:
		if m.streaming {
			return m, nil
		}
		notifications := teams.DrainLeadMailbox(m.teamMgr)
		if len(notifications) == 0 {
			return m, m.pollMailbox()
		}
		for _, note := range notifications {
			m.conversation.AddSystemReminder(note)
		}
		m.streaming = true
		m.thinking = true
		m.thinkingStart = time.Now()
		m.thinkingDone = 0
		m.streamBuf = ""
		m.toolBlocks = nil
		m.userScrolled = false
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelStream = cancel
		m.agentCh = m.ag.Run(ctx, m.conversation)
		m.updateViewport()
		return m, tea.Batch(
			m.listenForAgentEvents(),
			m.listenForAskUser(),
			m.listenForSubAgentProgress(),
			m.spinner.Tick,
		)
	}

	return m, nil
}

func (m Model) handleChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Dialog key dispatch follows the InteractionDock priority in
	// src/ui/interaction-dock.tsx (the permission dialog is handled above).
	if m.loginDialog {
		return m.handleLoginDialog(msg)
	}

	if m.providerDialog {
		return m.handleProviderDialog(msg)
	}

	if m.modelDialog {
		return m.handleModelDialog(msg)
	}

	if m.planApprovalDialog {
		return m.handlePlanApproval(msg)
	}

	if m.rewindDialog {
		return m.handleRewindKeys(msg)
	}

	if m.resumeDialog {
		return m.handleResumeDialog(msg)
	}

	if m.askUserDialog {
		return m.handleAskUserDialog(msg)
	}

	if m.sandboxDialog {
		return m.handleSandboxDialog(msg)
	}

	if msg.String() == "ctrl+b" && tools.HasAnyForegroundTasks(m.registry) {
		tools.BackgroundAllForegroundTasks(m.registry)
		return m, nil
	}

	if msg.String() == "ctrl+o" {
		toggled := false
		for i := range m.chatMessages {
			r := m.chatMessages[i].role
			if r == "tool_group" || r == "tool_collapsed" || r == "sub_agent" {
				m.chatMessages[i].expanded = !m.chatMessages[i].expanded
				toggled = true
			}
		}
		if toggled {
			m.updateViewport()
		}
		return m, nil
	}

	if msg.String() == "esc" && m.streaming && m.cancelStream != nil {
		m.interruptForeground()
		return m, nil
	}

	if m.atMenuOpen {
		switch msg.String() {
		case "up":
			if m.atCursor > 0 {
				m.atCursor--
			}
			return m, nil
		case "down":
			if m.atCursor < len(m.atMatches)-1 {
				m.atCursor++
			}
			return m, nil
		case "enter", "tab":
			if m.atCursor < len(m.atMatches) {
				selected := m.atMatches[m.atCursor]
				text := m.textarea.Value()
				atIdx := strings.LastIndex(text, "@")
				if atIdx >= 0 {
					m.textarea.Reset()
					m.textarea.SetHeight(1)
					m.textarea.InsertString(text[:atIdx] + "@" + selected + " ")
				}
				m.atMenuOpen = false
				m.atMatches = nil
				m.atCursor = 0
			}
			return m, nil
		case "esc":
			m.atMenuOpen = false
			m.atMatches = nil
			m.atCursor = 0
			return m, nil
		}
	}

	if m.slashMenuOpen {
		switch msg.String() {
		case "up":
			if m.slashCursor > 0 {
				m.slashCursor--
			}
			return m, nil
		case "down":
			if m.slashCursor < len(m.slashMatches)-1 {
				m.slashCursor++
			}
			return m, nil
		case "enter":
			if m.slashCursor < len(m.slashMatches) {
				selected := m.slashMatches[m.slashCursor]
				m.slashMenuOpen = false
				m.slashMatches = nil
				m.slashCursor = 0
				m.textarea.Reset()
				m.textarea.SetHeight(1)
				return m.executeCommand(selected.Name, "")
			}
			return m, nil
		case "esc":
			m.slashMenuOpen = false
			m.slashMatches = nil
			m.slashCursor = 0
			return m, nil
		case "tab":
			if m.slashCursor < len(m.slashMatches) {
				selected := m.slashMatches[m.slashCursor]
				m.textarea.Reset()
				m.textarea.SetHeight(1)
				m.textarea.InsertString("/" + selected.Name + " ")
				m.slashMenuOpen = false
				m.slashMatches = nil
				m.slashCursor = 0
			}
			return m, nil
		}
	}

	if msg.String() == "shift+tab" {
		if m.ag != nil && m.ag.Checker != nil && !m.streaming {
			m.ag.Checker.SetMode(nextPermissionMode(m.ag.Checker.Mode()))
			m.updateViewport()
		}
		return m, nil
	}

	if msg.String() == "ctrl+j" {
		m.textarea.InsertString("\n")
		m.resizeTextarea()
		return m, nil
	}

	if msg.String() == "enter" {
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		if m.streaming {
			if m.cancelStream != nil {
				m.cancelStream()
			}
			m.savePartialResponse()
			m.finishStreaming()
		}
		if strings.HasPrefix(text, "/") {
			name, args, ok := commands.Parse(text)
			if !ok {
				m.slashMenuOpen = false
				return m.sendMessage(text)
			}
			m.textarea.Reset()
			m.textarea.SetHeight(1)
			m.slashMenuOpen = false
			m.slashMatches = nil
			m.slashCursor = 0
			return m.executeCommand(name, args)
		}
		m.slashMenuOpen = false
		return m.sendMessage(text)
	}

	switch msg.String() {
	case "pgup", "pgdown", "home", "end":
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		m.userScrolled = !m.viewport.AtBottom()
		return m, vpCmd
	case "up":
		if !m.streaming && m.textarea.Line() == 0 {
			m.historyUp()
			return m, nil
		}
	case "down":
		if !m.streaming && m.textarea.Line() == m.textarea.LineCount()-1 {
			m.historyDown()
			return m, nil
		}
	}

	prevText := m.textarea.Value()
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	if m.textarea.Value() != prevText {
		m.historyIndex = 0
		m.resizeTextarea()
	}

	m.updateSlashMenu()
	m.updateAtMenu()

	return m, cmd
}

func (m Model) renderSlashMenu() string {
	var sb strings.Builder
	for i, cmd := range m.slashMatches {
		desc := cmd.Description
		if len([]rune(desc)) > 30 {
			desc = string([]rune(desc)[:28]) + "…"
		}
		label := fmt.Sprintf("  /%-16s — %s", cmd.Name, desc)
		if i == m.slashCursor {
			sb.WriteString(menuActiveStyle.Render(label) + "\n")
		} else {
			sb.WriteString(lipgloss.NewStyle().Foreground(dimText).Render(label) + "\n")
		}
	}
	return sb.String()
}

func (m Model) renderAtMenu() string {
	var sb strings.Builder
	for i, path := range m.atMatches {
		if i == m.atCursor {
			sb.WriteString(menuActiveStyle.Render("  "+path) + "\n")
		} else {
			sb.WriteString(lipgloss.NewStyle().Foreground(dimText).Render("  "+path) + "\n")
		}
	}
	return sb.String()
}
