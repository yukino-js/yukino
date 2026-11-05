package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/commands"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/history"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

func initializedHost(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m := newChatModel(t)
	m.activateInitialProvider(m.selectedProvider)
	if m.ag == nil {
		t.Fatal("host initialization failed")
	}
	t.Cleanup(func() {
		if err := m.Dispose(); err != nil {
			t.Error(err)
		}
	})
	return m
}

func TestHostProviderTransitionBlocksInputAndQueuesUntilCommit(t *testing.T) {
	t.Chdir(t.TempDir())
	m := initializedHost(t)
	oldClient, catalog, sessionID := m.client, m.skillCatalog, m.sessionID
	provider := config.ProviderConfig{Name: "next", Protocol: "openai", BaseURL: "https://example.invalid", Model: "next-model", APIKey: "test", ContextWindow: 64000, MaxOutputTokens: 2048}
	m.providers = append(m.providers, provider)
	next, cmd := m.switchProvider(provider)
	m = asModel(t, next)
	if !m.providerSwitching || m.client != oldClient || !strings.Contains(plainTerminalText(m.renderComposer(10)), "Switching provider...") {
		t.Fatal("provider was committed before the asynchronous change finished")
	}
	m.editor.SetValue("draft")
	next, _ = m.handleComposerKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = asModel(t, next)
	if m.editor.Value() != "draft" || m.wakeNotifications() != nil {
		t.Fatal("provider transition accepted input or started a notification turn")
	}
	next, _ = m.submitInput("/status")
	m = asModel(t, next)
	if len(m.pendingMessages) != 1 {
		t.Fatal("follow-up did not wait for the new client")
	}
	batch := cmd().(tea.BatchMsg)
	next, _ = m.Update(batch[len(batch)-1]())
	m = asModel(t, next)
	if m.providerSwitching || m.client == oldClient || m.ag.Client != m.client || m.ag.Protocol != "openai" || m.ag.ContextWindow != 64000 || m.ag.MaxOutputTokens != 2048 {
		t.Fatal("client and provider limits did not commit together")
	}
	if len(m.pendingMessages) != 0 || m.skillCatalog != catalog || m.sessionID != sessionID || !strings.Contains(m.chatMessages[len(m.chatMessages)-1].content, "Provider:  next (openai)") {
		t.Fatal("follow-up did not use the new provider or session resources were replaced")
	}
}

func TestHostProviderFailureKeepsRuntimeAndModelSaveFailureKeepsSwitch(t *testing.T) {
	t.Chdir(t.TempDir())
	m := initializedHost(t)
	oldClient, oldProvider := m.client, *m.selectedProvider
	next, cmd := m.switchProvider(config.ProviderConfig{Name: "invalid", Protocol: "invalid"})
	m = asModel(t, next)
	batch := cmd().(tea.BatchMsg)
	next, _ = m.Update(batch[len(batch)-1]())
	m = asModel(t, next)
	if m.providerSwitching || m.client != oldClient || m.selectedProvider.Name != oldProvider.Name || !strings.HasPrefix(m.activityError, "Failed to switch provider: ") {
		t.Fatal("failed switch changed the active runtime")
	}
	if err := os.MkdirAll(filepath.Dir(config.GlobalConfigPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.GlobalConfigPath(), []byte("providers: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	next, cmd = m.applyModelSelection("new-model")
	m = asModel(t, next)
	batch = cmd().(tea.BatchMsg)
	next, _ = m.Update(batch[len(batch)-1]())
	m = asModel(t, next)
	if m.providerSwitching || m.selectedProvider.Model != "new-model" || m.client == oldClient || !strings.HasPrefix(m.activityError, "Model switched but saving failed: ") {
		t.Fatal("saving failure rolled back the runtime switch")
	}
}

func TestHostStartupAndDialogResumeRecordCommands(t *testing.T) {
	t.Chdir(t.TempDir())
	m := initializedHost(t)
	targetID := session.NewID()
	if err := session.SaveMessage(m.getWorkDir(), targetID, session.Message{Role: "user", Content: "restored"}); err != nil {
		t.Fatal(err)
	}
	oldID := m.sessionID
	m.StartupResume = targetID
	next, _ := m.Update(struct{}{})
	m = asModel(t, next)
	entries, err := history.LoadInputs()
	if err != nil {
		t.Fatal(err)
	}
	if m.StartupResume != nil || m.sessionID != targetID || len(entries) != 1 || entries[0].Text != "/resume "+targetID || entries[0].SessionID == nil || *entries[0].SessionID != oldID || entries[0].CommandName == nil || *entries[0].CommandName != "resume" {
		t.Fatalf("startup resume history = %#v", entries)
	}
	next, _ = m.executeCommand("/resume", nil)
	m = asModel(t, next)
	next, _ = m.handleResumeDialog(tea.KeyMsg{Type: tea.KeyEnter})
	m = asModel(t, next)
	entries, err = history.LoadInputs()
	if err != nil {
		t.Fatal(err)
	}
	if m.resumeDialog || len(entries) != 3 || entries[2].Text != "/resume "+targetID || entries[2].CommandName == nil || *entries[2].CommandName != "resume" {
		t.Fatalf("dialog resume history = %#v", entries)
	}
}

func TestHostCommandFailureAndMCPQueueRemainOperable(t *testing.T) {
	t.Chdir(t.TempDir())
	m := initializedHost(t)
	m.cmdRegistry.Register(&commands.Command{Name: "fail", Type: commands.TypeLocal, Handler: func(*commands.Context) string { panic("fixture failure") }})
	next, _ := m.executeCommand("/fail", nil)
	m = asModel(t, next)
	if last := m.chatMessages[len(m.chatMessages)-1]; last.role != "system" || last.content != "Error: fixture failure" {
		t.Fatal(last)
	}
	if err := os.MkdirAll(filepath.Dir(config.GlobalConfigPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.GlobalConfigPath(), []byte("providers:\n  - name: test\n    protocol: anthropic\n    base_url: https://example.invalid\n    model: m\n    api_key: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.mcpConnecting = true
	next, _ = m.runMCPCommand("reload")
	m = asModel(t, next)
	next, _ = m.runMCPCommand("")
	m = asModel(t, next)
	if len(m.pendingMCPCommands) != 2 || !m.commandBusy {
		t.Fatal("MCP operations were not queued")
	}
	next, cmd := m.applyMCPResult(mcpReadyMsg{mgr: mcp.NewManager(false)})
	m = asModel(t, next)
	if len(m.pendingMCPCommands) != 1 || !m.commandBusy || !m.mcpConnecting {
		t.Fatal("queued reload did not own the MCP slot")
	}
	next, _ = m.Update(cmd())
	m = asModel(t, next)
	if m.mcpConnecting || m.commandBusy || len(m.pendingMCPCommands) != 0 || m.chatMessages[len(m.chatMessages)-1].content != "No MCP servers configured." {
		t.Fatal("MCP queue did not settle in order")
	}
	if err := os.WriteFile(config.GlobalConfigPath(), []byte("providers: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.mcpConnecting = true
	m.pendingMCPCommands = []string{"reload", ""}
	next, _ = m.applyMCPResult(mcpReadyMsg{})
	m = asModel(t, next)
	if m.commandBusy || len(m.pendingMCPCommands) != 0 || m.chatMessages[len(m.chatMessages)-1].content != "No MCP servers configured." {
		t.Fatal("failed MCP operation stranded the queue")
	}
}

func TestHostRecallAndMemoryCallbacksFollowSession(t *testing.T) {
	t.Chdir(t.TempDir())
	m := initializedHost(t)
	first := m.newRunAgent(context.Background())
	first.RecordRecentTool("ReadFile")
	first.MarkMemoriesSurfaced([]string{"memory.md"})
	second := m.newRunAgent(context.Background())
	recent, surfaced := second.RecallHints()
	if len(recent) != 1 || recent[0] != "ReadFile" {
		t.Fatal(recent)
	}
	if _, ok := surfaced["memory.md"]; !ok {
		t.Fatal("next run lost surfaced memories")
	}
	oldGeneration := m.memoryGeneration
	next, _ := m.executeCommand("/clear", nil)
	m = asModel(t, next)
	count := len(m.chatMessages)
	next, _ = m.handleMemoryLoop(memoryLoopMsg{conv: m.conversation, generation: oldGeneration})
	m = asModel(t, next)
	next, _ = m.handleMemorySystem(memorySystemMsg{message: "stale", generation: oldGeneration})
	m = asModel(t, next)
	if m.memoryTasks != nil || len(m.chatMessages) != count {
		t.Fatal("stale memory callbacks survived clear")
	}
	if recent, surfaced := m.newRunAgent(context.Background()).RecallHints(); len(recent) != 0 || len(surfaced) != 0 {
		t.Fatal("recall hints survived clear")
	}
}

func TestHostPlanCompletionOpensApprovalOnlyOnSuccess(t *testing.T) {
	t.Chdir(t.TempDir())
	m := initializedHost(t)
	m.streaming = true
	m.ag.Checker.EnterPlanMode()
	next, _ := m.handleAgentEvent(agent.ToolResultEvent{ToolID: "plan", ToolName: "ExitPlanMode", IsError: true})
	m = asModel(t, next)
	next, _ = m.handleAgentEvent(agent.LoopComplete{StopReason: "end_turn"})
	m = asModel(t, next)
	if m.planApprovalDialog {
		t.Fatal("failed plan tool opened approval")
	}
	m.streaming = true
	next, _ = m.handleAgentEvent(agent.ToolResultEvent{ToolID: "plan", ToolName: "ExitPlanMode"})
	m = asModel(t, next)
	next, _ = m.handleAgentEvent(agent.LoopComplete{StopReason: "end_turn"})
	m = asModel(t, next)
	if !m.planApprovalDialog || m.ag.Checker.Mode() != permissions.ModePlan {
		t.Fatal("successful plan did not open approval")
	}
}

func TestHostSandboxCacheFollowsSessionAndOff(t *testing.T) {
	t.Chdir(t.TempDir())
	m := initializedHost(t)
	next, _ := m.runSandboxCommand("auto")
	m = asModel(t, next)
	tool := m.registry.Get("Bash").(*tools.BashTool)
	m.configureBashSandbox(tool)
	first := m.sandboxRuntime.Get()
	next, _ = m.executeCommand("/clear", nil)
	m = asModel(t, next)
	if !m.sandboxEnabled || m.sandboxRuntime.Get() != first {
		t.Fatal("clear discarded the sandbox mode or instance")
	}
	next, _ = m.doResumeSession(m.getWorkDir(), session.NewID(), []session.Message{{Role: "user", Content: "restored"}})
	m = asModel(t, next)
	if m.sandboxRuntime.Get() != first {
		t.Fatal("resume discarded the sandbox instance")
	}
	next, _ = m.runSandboxCommand("off")
	m = asModel(t, next)
	if m.sandboxEnabled || m.sandboxAutoAllow || tool.Sandbox != nil || tool.SandboxRequired {
		t.Fatal("off did not clear sandbox mode and tool binding")
	}
}

func TestHostClearResetsConversationState(t *testing.T) {
	m := initializedHost(t)
	oldSession := m.sessionID
	cachePath := filepath.Join(t.TempDir(), "read.txt")
	m.ag.FileStateCache.Record(cachePath, 1)
	m.ag.RecordRecentTool("ReadFile")
	m.ag.MarkMemoriesSurfaced([]string{"memory.md"})
	m.ag.Checker.SetPlanFilePath("old-plan.md")
	m.ag.ActivateSkill("active", "skill body")
	m.pendingMessages = []queuedMessage{{text: "old queue"}}
	next, _ := m.executeCommand("/clear", nil)
	m = asModel(t, next)
	if m.sessionID == oldSession || m.ag.Checker.CurrentPlanPath() != "" || m.ag.FileStateCache.Has(cachePath) || len(m.pendingMessages) != 0 {
		t.Fatal("clear retained prior session state")
	}
	if recent, surfaced := m.ag.RecallHints(); len(recent) != 0 || len(surfaced) != 0 {
		t.Fatal("recall state survived clear")
	}
	if m.ag.GetActiveSkills()["active"] != "skill body" {
		t.Fatal("clear dropped the app's active skill cache")
	}
}

func TestHostResumeKeepsExcludedBashCard(t *testing.T) {
	m := initializedHost(t)
	result := conversation.UserBashResult{Command: "echo private", Output: "private", ExcludeFromContext: true, Status: "completed"}
	saved := []session.Message{{Role: "user", Content: conversation.UserBashContext(result), UserBash: &result}}
	next, _ := m.doResumeSession(m.getWorkDir(), session.NewID(), saved)
	m = asModel(t, next)
	if len(m.chatMessages) != 2 || len(m.chatMessages[0].toolGroup) != 1 || m.chatMessages[0].toolGroup[0].progress != UserBashProgressExcluded {
		t.Fatalf("excluded Bash card was not restored: %#v", m.chatMessages)
	}
	for _, message := range m.conversation.GetMessages() {
		if strings.Contains(message.ContentText(), "private") {
			t.Fatal("excluded Bash output entered model context")
		}
	}
}

func TestHostForkSkillIsForegroundAndCancelable(t *testing.T) {
	m := initializedHost(t)
	root := t.TempDir()
	directory := filepath.Join(root, ".agents", "skills", "fork-test")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: fork-test\ndescription: fork test\nmode: fork\n---\nRun task"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.skillCatalog = skills.LoadCatalog(root)
	bootstrapCount := len(m.chatMessages)
	next, cmd := m.startSkillFork("fork-test", "arguments")
	m = asModel(t, next)
	if !m.streaming || !m.foregroundBusy() || m.cancelStream == nil {
		t.Fatal("fork skill did not own foreground execution")
	}
	if len(m.chatMessages) != bootstrapCount+1 || m.chatMessages[bootstrapCount].role != "system" || m.chatMessages[bootstrapCount].content != "Running skill \"fork-test\" in fork mode…" {
		t.Fatal("fork notice differs from the reference")
	}
	m.interruptForeground()
	commands := cmd().(tea.BatchMsg)
	finished := commands[len(commands)-1]().(forkSkillDoneMsg)
	if !errors.Is(finished.err, context.Canceled) {
		t.Fatalf("fork did not receive cancellation: %v", finished.err)
	}
	next, _ = m.update(finished)
	m = asModel(t, next)
	if m.streaming || m.cancelStream != nil {
		t.Fatal("fork completion retained foreground ownership")
	}
}

func TestHostLocalCommandsRecordHistoryOnce(t *testing.T) {
	m := initializedHost(t)
	next, _ := m.dispatchInput("/status", nil)
	m = asModel(t, next)
	entries, err := history.LoadInputs()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Text != "/status" || entries[0].CommandName == nil || *entries[0].CommandName != "status" {
		t.Fatalf("command history = %#v", entries)
	}
}

func TestHostQueuedAskCancellationDoesNotWaitForHead(t *testing.T) {
	m := newChatModel(t)
	first, second := make(chan tools.QuestionResponse, 1), make(chan tools.QuestionResponse, 1)
	ctx, cancel := context.WithCancel(context.Background())
	m.enqueueAsk(tools.AskUserRequest{Questions: []tools.Question{{Text: "first"}}, ResponseCh: first})
	m.enqueueAsk(tools.AskUserRequest{Context: ctx, Questions: []tools.Question{{Text: "second"}}, ResponseCh: second})
	cancel()
	m.pruneInteractions()
	select {
	case answer := <-second:
		if len(answer.Answers) != 0 {
			t.Fatal(answer)
		}
	default:
		t.Fatal("canceled queued ask is still waiting behind the active dialog")
	}
	if !m.askUserDialog || m.askUserRespCh != first {
		t.Fatal("canceling a queued ask displaced the active dialog")
	}
	m.denyAllInteractions()
}
