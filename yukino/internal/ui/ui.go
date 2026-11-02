package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/commands"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/file_history"
	"github.com/yukino-js/yukino/yukino/internal/goal"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
	"github.com/yukino-js/yukino/yukino/internal/memory"
	"github.com/yukino-js/yukino/yukino/internal/memory/background"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
	"github.com/yukino-js/yukino/yukino/internal/teams"
	"github.com/yukino-js/yukino/yukino/internal/todo"
	"github.com/yukino-js/yukino/yukino/internal/tools"
	"github.com/yukino-js/yukino/yukino/internal/vscode"

	"github.com/charmbracelet/bubbles/spinner"
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

	state         appState
	streaming     bool
	startingAgent bool
	settlingCh    <-chan agent.AgentEvent
	settleWaiting bool

	editor   inputEditor
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

	atMenuOpen         bool
	atMatches          []string
	atCursor           int
	atPrefix           string
	dropdownDismissed  bool
	recentCommandCount int
	fileCache          []string
	fileCacheCwd       string
	fileCacheTime      time.Time
	fileFactsVersion   int
	fileCacheVersion   int
	pasteError         string
	pastingImage       bool
	pasteGeneration    int
	ctrlCDeadline      time.Time
	ctrlCGeneration    int
	toolsExpanded      bool
	pendingMessages    []string
	queuePaused        bool
	retryStatus        string
	activityError      string
	compacting         bool

	spinner       spinner.Model
	thinking      bool
	thinkingStart time.Time
	thinkingDone  float64

	instructionsContent string
	memoryContent       string

	mcpConfigs    []config.MCPServerConfig
	mcpMgr        *mcp.Manager
	mcpConnecting bool
	mcpAnnounced  map[string]bool
	mcpServerInfo string
	hookConfigs   []config.HookConfig

	historyEntries []string
	historyIndex   int
	historyDraft   *inputSnapshot

	sessionID  string
	goalBinder *goalBinder

	ideMentionCh chan vscode.IdeAtMention
	ideConn      *vscode.IdeConnection
	ideCancel    context.CancelFunc
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
	memoryTasks        *background.Tasks
	teamMgr            *teams.TeamManager

	sandboxDialog         bool
	sandboxCursor         int
	sandboxCfg            *config.SandboxYamlConfig
	EnableCoordinatorMode bool
	MCPLoading            string
	// permissionMode is the startup mode from config.yaml (`permission_mode:`);
	// plan is not a valid startup mode and falls back to default.
	permissionMode permissions.PermissionMode
	// memoryEnabled mirrors config.yaml `enable_memory:`; it gates the whole
	// auto-memory pipeline (injection, recall, extraction, consolidation).
	memoryEnabled bool

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

func (m Model) dialogAvailableRows() int {
	_, _, rows := m.terminalBudgets()
	return rows
}

// initialPermissionMode resolves the config.yaml `permission_mode:` value to a
// startup mode. Plan is excluded: it is entered through the mode cycle or the
// ExitPlanMode tool, never at startup.
func initialPermissionMode(mode string) permissions.PermissionMode {
	switch permissions.PermissionMode(mode) {
	case permissions.ModeDefault, permissions.ModeAcceptEdits, permissions.ModeBypass:
		return permissions.PermissionMode(mode)
	}
	return permissions.ModeDefault
}

func New(providers []config.ProviderConfig, mcpConfigs []config.MCPServerConfig, hookConfigs []config.HookConfig, sandboxCfg *config.SandboxYamlConfig, defaultProvider int, permissionMode string, memoryEnabled bool) Model {
	ta := newInputEditor()

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
	binder := &goalBinder{}
	reg.Register(&tools.GoalTool{Manager: func() *goal.GoalManager { return binder.get() }})

	sCfg := sandboxCfg
	initialTodos, _ := todo.NewTaskList(nil)

	m := Model{
		providers:          providers,
		todoList:           initialTodos,
		mcpConfigs:         mcpConfigs,
		mcpAnnounced:       map[string]bool{},
		hookConfigs:        hookConfigs,
		sandboxCfg:         sCfg,
		state:              stateProviderSelect,
		defaultProvider:    defaultProvider,
		providerCursor:     defaultProvider,
		permissionMode:     initialPermissionMode(permissionMode),
		memoryEnabled:      memoryEnabled,
		editor:             ta,
		conversation:       conversation.NewManager(),
		registry:           reg,
		defaultTools:       dt,
		cmdRegistry:        newUICommandRegistry(),
		spinner:            sp,
		askUserCh:          askCh,
		subAgentProgressCh: subProgressCh,
		goalBinder:         binder,
		ideMentionCh:       make(chan vscode.IdeAtMention, 16),
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
	cmds := []tea.Cmd{m.connectIde()}
	if len(m.providers) > 0 {
		cmds = append(cmds, func() tea.Msg { return initSingleProviderMsg{} })
	}
	return tea.Batch(cmds...)
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.editor.SetWidth(msg.Width - boolColumn(msg.Width > 2)*2)

		statusHeight := 1
		sepHeight := 2
		inputHeight := m.editor.Height() + 1
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

	case ideConnectedMsg:
		m.ideConn = msg.connection
		m.ideCancel = msg.cancel
		return m, m.listenForIdeMentions()

	case ideAtMentionMsg:
		wd, _ := os.Getwd()
		text := ideMentionText(wd, msg.mention)
		before := []rune(m.editor.lines[m.editor.line])[:m.editor.col]
		if len(before) > 0 && !unicode.IsSpace(before[len(before)-1]) {
			text = " " + text
		}
		m.editor.InsertString(text)
		m.resizeInput()
		m.updateSlashMenu()
		m.updateAtMenu()
		return m, m.listenForIdeMentions()

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
		m.compacting = false
		if m.cancelStream != nil {
			m.cancelStream()
			m.cancelStream = nil
		}
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
		m.mcpMgr = msg.mgr
		for _, connected := range msg.result.Tools {
			m.registry.Register(connected.Tool)
		}
		var mcpPrintLines []string
		for _, connectError := range msg.result.Errors {
			m.chatMessages = append(m.chatMessages, chatMessage{
				role:    "error",
				content: connectError.ServerName + ": " + connectError.Error,
			})
			mcpPrintLines = append(mcpPrintLines, errorStyle.Render("✖ "+connectError.ServerName+": "+connectError.Error))
		}
		if p := m.selectedProvider; p != nil {
			mcp.DecideAndApply(m.registry, p.BaseURL, p.Protocol, config.GetContextWindow(p), m.MCPLoading)
		}
		registered := len(msg.result.Tools)
		if registered > 0 {
			m.mcpServerInfo = fmt.Sprintf("Connected to %d MCP server(s), %d tools registered", len(msg.result.Servers), registered)
		}
		mcp.SyncMcpInstructions(m.conversation, m.mcpAnnounced, m.mcpMgr)
		m.committedUpTo = len(m.chatMessages)
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
		m.startingAgent = false
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

	return m.handleComposerKey(msg)
}
