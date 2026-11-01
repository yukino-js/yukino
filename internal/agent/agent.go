package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/compact"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/file_history"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/hooks"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/plan_file"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/prompt"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tool_result"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/utils"
)

const (
	maxTokensCeiling          = 64000
	maxOutputTokensRecoveries = 3
)

type Agent struct {
	Client              llm.Client
	Registry            *tools.Registry
	Protocol            string
	WorkDir             string
	MaxIterations       int
	ContextWindow       int
	MaxOutputTokens     int
	Checker             *permissions.Checker
	Hooks               *hooks.Engine
	SessionID           string
	TaskManager         tools.BackgroundTaskManager
	DisableBackground   bool
	NotificationFn      func() []string
	Instructions        string
	MemoryContent       string
	SkillSection        string
	SkillDeltaFn        func() string
	MemoryRecallCh      <-chan RecallResult
	ToolNameFilter      func(name string) bool
	CoordinatorActiveFn func() bool
	OnLoopComplete      func(conv *conversation.Manager)
	FileHistory         *file_history.History
	FileStateCache      *tools.FileStateCache
	compactTracking     compact.AutoCompactTrackingState
	RecoveryState       *compact.RecoveryState
	eventCh             chan AgentEvent
	activeSkills        map[string]string
	announcedDeferred   []string
	recallMu            sync.Mutex
	recentToolNames     []string
	surfacedMemPaths    map[string]struct{}
}

type RecallResult struct {
	Reminder string
	Paths    []string
}

const maxRecentTools = 10

func (a *Agent) RecordRecentTool(name string) {
	if name == "" {
		return
	}
	a.recallMu.Lock()
	defer a.recallMu.Unlock()
	if i := slices.Index(a.recentToolNames, name); i >= 0 {
		a.recentToolNames = slices.Delete(a.recentToolNames, i, i+1)
	}
	a.recentToolNames = append(a.recentToolNames, name)
	if len(a.recentToolNames) > maxRecentTools {
		a.recentToolNames = a.recentToolNames[1:]
	}
}

func (a *Agent) RecallHints() ([]string, map[string]struct{}) {
	a.recallMu.Lock()
	defer a.recallMu.Unlock()
	tools := slices.Clone(a.recentToolNames)
	surfaced := make(map[string]struct{}, len(a.surfacedMemPaths))
	for p := range a.surfacedMemPaths {
		surfaced[p] = struct{}{}
	}
	return tools, surfaced
}

func (a *Agent) MarkMemoriesSurfaced(paths []string) {
	if len(paths) == 0 {
		return
	}
	a.recallMu.Lock()
	defer a.recallMu.Unlock()
	if a.surfacedMemPaths == nil {
		a.surfacedMemPaths = make(map[string]struct{}, len(paths))
	}
	for _, p := range paths {
		a.surfacedMemPaths[p] = struct{}{}
	}
}

const deferredReminderMarker = "The following deferred tools are available via ToolSearch."

func (a *Agent) ActivateSkill(name, body string) {
	if a.activeSkills == nil {
		a.activeSkills = make(map[string]string)
	}
	a.activeSkills[name] = body
}

func (a *Agent) ClearActiveSkills() {
	a.activeSkills = nil
}

func (a *Agent) GetActiveSkills() map[string]string {
	out := make(map[string]string, len(a.activeSkills))
	for k, v := range a.activeSkills {
		out[k] = v
	}
	return out
}

func (a *Agent) SetToolFilter(allow func(name string) bool) {
	a.ToolNameFilter = allow
}

func (a *Agent) ToolRegistry() *tools.Registry {
	return a.Registry
}

func New(client llm.Client, registry *tools.Registry, protocol string) *Agent {
	wd, _ := os.Getwd()
	return &Agent{
		Client:         client,
		Registry:       registry,
		Protocol:       protocol,
		WorkDir:        wd,
		MaxIterations:  0,
		ContextWindow:  200000,
		RecoveryState:  compact.NewRecoveryState(),
		FileStateCache: tools.NewFileStateCache(),
	}
}

func (a *Agent) SetSessionID(id string) { a.SessionID = id }

func (a *Agent) currentToolSchemas() []map[string]any {
	schemas := a.Registry.GetAllSchemas(a.Protocol)
	if a.ToolNameFilter == nil {
		return schemas
	}
	return filterSchemasByName(schemas, a.ToolNameFilter)
}

func (a *Agent) Run(ctx context.Context, conv *conversation.Manager) <-chan AgentEvent {
	ch := make(chan AgentEvent, 32)

	go func() {
		defer close(ch)
		defer a.emitHook(hooks.EventSessionEnd, "", nil)

		a.emitHook(hooks.EventSessionStart, "", nil)

		conv.InjectLongTermMemory(a.Instructions, a.MemoryContent, a.SkillSection)

		var totalInput, totalOutput int
		maxTokensEscalated := false
		outputRecoveries := 0

		for iteration := 1; ; iteration++ {
			if a.MaxIterations > 0 && iteration > a.MaxIterations {
				ch <- ErrorEvent{Message: fmt.Sprintf("Agent reached maximum iterations (%d)", a.MaxIterations)}
				return
			}

			if ctx.Err() != nil {
				return
			}

			toolSchemas := a.currentToolSchemas()

			if a.Checker != nil && a.Checker.Mode() == permissions.ModePlan {
				planPath := plan_file.GetOrCreatePlanPath()
				a.Checker.PlanFilePath = planPath
				planExists := plan_file.PlanExists()
				reminder := prompt.BuildPlanModeReminder(planPath, planExists, iteration)
				conv.AddSystemReminder(reminder)
			}

			if a.CoordinatorActiveFn != nil && a.CoordinatorActiveFn() {
				conv.AddSystemReminder(prompt.CoordinatorReminder(iteration))
			}

			// Drain queued hook notifications and any external notifications
			// (e.g. a team mailbox) into system reminders for this turn.
			if a.Hooks != nil {
				for _, note := range a.Hooks.DrainNotifications() {
					conv.AddSystemReminder(note)
				}
			}
			if a.NotificationFn != nil {
				for _, note := range a.NotificationFn() {
					conv.AddSystemReminder(note)
				}
			}

			a.emitHook(hooks.EventTurnStart, "", nil)

			if a.SkillDeltaFn != nil {
				if delta := a.SkillDeltaFn(); delta != "" {
					conv.AddSystemReminder("The following skills became available:\n" + delta)
				}
			}

			if deferredNames := a.Registry.GetDeferredToolNames(); len(deferredNames) > 0 {
				poolChanged := !slices.Equal(a.announcedDeferred, deferredNames)
				if poolChanged || !conv.HasReminderContaining(deferredReminderMarker) {
					reminder := deferredReminderMarker + " Their schemas are NOT loaded - use ToolSearch with query \"select:<name>[,<name>...]\" to load tool schemas"
					if a.Registry.McpLoadingMode == tools.McpLoadingDispatch {
						reminder += ", then invoke them with the McpCall tool"
					} else {
						reminder += " before calling them"
					}
					conv.AddSystemReminder(reminder + ":\n" + strings.Join(deferredNames, "\n"))
					a.announcedDeferred = deferredNames
				}
			}

			a.emitHook(hooks.EventPreSend, "", nil)

			// Pre-send and turn-start prompts must reach the request they
			// prepare, so drain the once-notifications written by those hooks
			// into this turn as well.
			if a.Hooks != nil {
				for _, note := range a.Hooks.DrainNotifications() {
					conv.AddSystemReminder(note)
				}
			}

			if msg, err := compact.ManageContext(ctx, conv, a.Client, a.WorkDir, a.SessionID, a.ContextWindow, a.MaxOutputTokens, &a.compactTracking, a.RecoveryState, toolSchemas); err == nil && msg != "" {
				ch <- CompactEvent{Message: msg}
				conv.ClearUsageAnchor()
				conv.InjectLongTermMemory(a.Instructions, a.MemoryContent, a.SkillSection)
			}

			events, errs := a.Client.Stream(ctx, conv, toolSchemas)

			var text string
			var toolCalls []llm.ToolCallComplete
			var thinkingBlocks []conversation.ThinkingBlock
			var stopReason string
			var usage llm.UsageInfo

			executor := NewStreamingExecutor(a.Registry, ch)

			for ev := range events {
				switch e := ev.(type) {
				case llm.ThinkingDelta:
					ch <- ThinkingText{Text: e.Text}
				case llm.ThinkingComplete:
					thinkingBlocks = append(thinkingBlocks, conversation.ThinkingBlock{
						Thinking:  e.Thinking,
						Signature: e.Signature,
					})
				case llm.TextDelta:
					text += e.Text
					ch <- StreamText{Text: e.Text}
				case llm.ToolCallStart:
					ch <- ToolUseEvent{ToolID: e.ToolID, ToolName: e.ToolName}
				case llm.ToolCallDelta:
				case llm.ToolCallComplete:
					toolCalls = append(toolCalls, e)
					ch <- ToolUseEvent{
						ToolID:   e.ToolID,
						ToolName: e.ToolName,
						Args:     e.Arguments,
					}
					executor.Submit(toolCallInfo{
						toolID:    e.ToolID,
						toolName:  e.ToolName,
						arguments: e.Arguments,
					})
				case llm.StreamEnd:
					stopReason = e.StopReason
					usage = e.Usage
				}
			}
			a.emitHook(hooks.EventPostReceive, text, nil)

			select {
			case err := <-errs:
				if err != nil {
					if retry, compacted := a.handleStreamError(ctx, ch, conv, err); retry {
						if compacted {
							conv.ClearUsageAnchor()
							conv.InjectLongTermMemory(a.Instructions, a.MemoryContent, a.SkillSection)
						}
						continue
					}
					ch <- ErrorEvent{Message: err.Error()}
					return
				}
			default:
			}

			totalInput += usage.InputTokens
			totalOutput += usage.OutputTokens
			ch <- UsageEvent{InputTokens: totalInput, OutputTokens: totalOutput}

			anchorAfterAssistant := func() {
				conv.RecordUsageAnchor(
					usage.InputTokens,
					usage.OutputTokens,
					usage.CacheReadTokens,
					usage.CacheCreationTokens,
				)
			}

			if stopReason == "max_tokens" {
				if !maxTokensEscalated {
					if setter, ok := a.Client.(llm.MaxTokensSetter); ok {
						setter.SetMaxOutputTokens(maxTokensCeiling)
						maxTokensEscalated = true
					}
					if text != "" {
						conv.AddAssistantFull(text, thinkingBlocks, nil)
						a.persistLastMessage(conv)
						anchorAfterAssistant()
						conv.AddUserMessage("Output token limit hit. Resume directly from where you stopped. Do not apologize or repeat previous content. Pick up mid-thought if needed.")
					}
					ch <- RetryEvent{Reason: "max_tokens escalation", Wait: 0}
					continue
				} else if outputRecoveries < maxOutputTokensRecoveries {
					outputRecoveries++
					conv.AddAssistantFull(text, thinkingBlocks, nil)
					a.persistLastMessage(conv)
					anchorAfterAssistant()
					conv.AddUserMessage("Output token limit hit. Resume directly from where you stopped. Break remaining work into smaller pieces.")
					ch <- RetryEvent{Reason: fmt.Sprintf("max_tokens recovery %d/%d", outputRecoveries, maxOutputTokensRecoveries), Wait: 0}
					continue
				}
			} else {
				outputRecoveries = 0
			}

			if len(toolCalls) == 0 {
				conv.AddAssistantFull(text, thinkingBlocks, nil)
				a.persistLastMessage(conv)
				if a.FileHistory != nil {
					summary := text
					if len(summary) > 60 {
						summary = summary[:60] + "..."
					}
					a.FileHistory.MakeSnapshot(conv.Len(), summary)
				}
				ch <- LoopComplete{TotalTurns: iteration}
				if a.OnLoopComplete != nil {
					go a.OnLoopComplete(conv)
				}
				return
			}

			var toolUses []conversation.ToolUseBlock
			for _, tc := range toolCalls {
				toolUses = append(toolUses, conversation.ToolUseBlock{
					ToolUseID: tc.ToolID,
					ToolName:  tc.ToolName,
					Arguments: tc.Arguments,
				})
			}
			conv.AddAssistantFull(text, thinkingBlocks, toolUses)
			a.persistLastMessage(conv)
			anchorAfterAssistant()

			results := executor.ExecuteAll(ctx, a)

			exempt := make(map[string]bool)
			for _, tc := range toolCalls {
				if tool_result.IsSpillReadback(tc.ToolName, tc.Arguments, a.WorkDir, a.SessionID) {
					exempt[tc.ToolID] = true
				}
			}

			var toolResults []conversation.ToolResultBlock
			for _, r := range results {
				ch <- ToolResultEvent{
					ToolID:   r.toolID,
					ToolName: r.toolName,
					Output:   r.output,
					IsError:  r.isError,
					Elapsed:  r.elapsed,
				}

				content := r.output
				if len(content) > tools.MaxOutputChars && !exempt[r.toolID] {
					content = tool_result.PersistLargeResult(a.SessionID, r.toolID, r.output)
					exempt[r.toolID] = true
				}
				toolResults = append(toolResults, conversation.ToolResultBlock{
					ToolUseID:     r.toolID,
					Content:       content,
					IsError:       r.isError,
					ContentBlocks: r.contentBlocks,
				})
			}

			tool_result.ApplyBudget(toolResults, a.SessionID, exempt)

			exitPlanCalled := false
			for _, tc := range toolCalls {
				if tc.ToolName == "ExitPlanMode" {
					exitPlanCalled = true
					break
				}
			}
			conv.AddToolResultsMessage(toolResults)
			a.persistLastMessage(conv)

			if a.MemoryRecallCh != nil {
				select {
				case recall := <-a.MemoryRecallCh:
					if recall.Reminder != "" {
						conv.AddSystemReminder(recall.Reminder)
						a.MarkMemoriesSurfaced(recall.Paths)
					}
					a.MemoryRecallCh = nil
				default:
				}
			}

			if exitPlanCalled {
				ch <- TurnComplete{Turn: iteration}
				ch <- LoopComplete{TotalTurns: iteration}
				return
			}
			ch <- TurnComplete{Turn: iteration}
			a.emitHook(hooks.EventTurnEnd, "", nil)
		}
	}()

	return ch
}

func (a *Agent) emitHook(event hooks.EventName, message string, args map[string]any) {
	if a.Hooks == nil {
		return
	}
	a.Hooks.Fire(event, hooks.HookContext{
		Event:   event,
		Args:    args,
		Message: message,
	}, hooks.HookRuntimeOptions{Cwd: a.WorkDir})
}

func filterSchemasByName(schemas []map[string]any, allow func(name string) bool) []map[string]any {
	out := make([]map[string]any, 0, len(schemas))
	for _, s := range schemas {
		name := tools.SchemaName(s)
		if allow(name) {
			out = append(out, s)
		}
	}
	return out
}

func (a *Agent) handleStreamError(ctx context.Context, ch chan AgentEvent, conv *conversation.Manager, err error) (retry, compacted bool) {
	var ctxErr *llm.ContextTooLongError
	if errors.As(err, &ctxErr) {
		msg, compactErr := compact.ForceCompact(ctx, conv, a.Client, a.WorkDir, a.SessionID, a.ContextWindow, a.RecoveryState, a.currentToolSchemas())
		if compactErr == nil && msg != "" {
			ch <- CompactEvent{Message: "Auto-compacted due to context length: " + msg}
			return true, true
		}
		return false, false
	}

	var rlErr *llm.RateLimitError
	if errors.As(err, &rlErr) {
		wait := parseRetryAfter(rlErr.RetryAfter)
		ch <- RetryEvent{Reason: "rate limited", Wait: wait}
		select {
		case <-time.After(wait):
			return true, false
		case <-ctx.Done():
			return false, false
		}
	}

	return false, false
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 5 * time.Second
	}
	if secs, err := strconv.Atoi(header); err == nil {
		return time.Duration(secs) * time.Second
	}
	return 5 * time.Second
}

type toolExecResult struct {
	toolID        string
	toolName      string
	output        string
	isError       bool
	elapsed       time.Duration
	contentBlocks []map[string]any
}

func extractFilePath(args map[string]any) string {
	for _, key := range []string{"file_path", "path", "pattern", "target"} {
		if v, ok := args[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func (a *Agent) executeSingleTool(ctx context.Context, eventCh chan AgentEvent, tc toolCallInfo) toolExecResult {
	tool := a.Registry.Get(tc.toolName)
	start := time.Now()

	if tool == nil {
		return toolExecResult{
			toolID:   tc.toolID,
			toolName: tc.toolName,
			output:   fmt.Sprintf("Error: unknown tool '%s'", tc.toolName),
			isError:  true,
			elapsed:  time.Since(start),
		}
	}

	if a.Checker != nil {
		decision := a.Checker.Check(tool, tc.arguments)
		if decision.Effect == permissions.Deny {
			return toolExecResult{
				toolID:   tc.toolID,
				toolName: tc.toolName,
				output:   fmt.Sprintf("Permission denied: %s", decision.Reason),
				isError:  true,
				elapsed:  time.Since(start),
			}
		}
		if decision.Effect == permissions.Ask {
			respCh := make(chan PermissionReply, 1)
			desc := permissions.DescribeToolAction(tc.toolName, tc.arguments)
			eventCh <- PermissionRequestEvent{
				ToolName:   tc.toolName,
				Desc:       desc,
				ResponseCh: respCh,
			}
			var reply PermissionReply
			select {
			case reply = <-respCh:
			case <-ctx.Done():
				reply = PermissionReply{Response: PermDeny}
			}
			if reply.Response == PermDeny {
				return toolExecResult{
					toolID:   tc.toolID,
					toolName: tc.toolName,
					output:   conversation.RejectedToolResultWithFeedback(reply.Feedback),
					isError:  true,
					elapsed:  time.Since(start),
				}
			}
			if reply.Response == PermAllowAlways {
				if err := a.Checker.AllowAlways(tc.toolName, tc.arguments, nil); err != nil {
					return toolExecResult{toolID: tc.toolID, toolName: tc.toolName, output: fmt.Sprintf("Error saving permission rule: %s", err), isError: true, elapsed: time.Since(start)}
				}
			}
		}
	}

	approvedMode := permissions.PermissionMode("")
	if a.Checker != nil {
		approvedMode = a.Checker.Mode()
	}
	if a.Hooks != nil {
		if rejected, msg := a.Hooks.FirePreToolHooks(tc.toolName, tc.arguments, hooks.HookRuntimeOptions{Cwd: a.WorkDir}); rejected {
			return toolExecResult{
				toolID:   tc.toolID,
				toolName: tc.toolName,
				output:   "Blocked by hook: " + msg,
				isError:  true,
				elapsed:  time.Since(start),
			}
		}
	}

	ctx = tools.WithExecutionContext(ctx, tools.ExecutionContext{IsAgentRuntime: true, FileStateCache: a.FileStateCache, FileHistory: a.FileHistory, WorkDir: a.WorkDir, SessionID: a.SessionID, ToolCallID: tc.toolID, TaskManager: a.TaskManager, DisableBackground: a.DisableBackground, CanReadSearchPath: func(path string) bool { return a.Checker == nil || a.Checker.CanReadSearchPath(path) }, CanTraverseSearchPath: func(path string) bool { return a.Checker == nil || a.Checker.CanTraverseSearchPath(path) }, CheckPermission: func(name string, args map[string]any) *tools.ToolResult {
		if a.Checker == nil {
			return nil
		}
		decision := a.Checker.Check(tool, args)
		if decision.Effect == permissions.Deny || decision.Effect == permissions.Ask && a.Checker.Mode() != approvedMode {
			return &tools.ToolResult{Output: fmt.Sprintf("Permission changed while waiting: %s. Request approval again before retrying.", decision.Reason), IsError: true}
		}
		return nil
	}})
	result := tool.Execute(ctx, tc.arguments)

	a.RecordRecentTool(tc.toolName)

	if !result.IsError && tc.toolName == "ReadFile" {
		if p, _ := tc.arguments["file_path"].(string); p != "" {
			p = utils.ResolveToolPath(a.WorkDir, p)
			if data, err := os.ReadFile(p); err == nil {
				a.RecoveryState.RecordFileRead(p, string(data))
			}
		}
	}

	if a.Hooks != nil {
		a.Hooks.Fire(hooks.EventPostToolUse, hooks.HookContext{
			Event:    hooks.EventPostToolUse,
			ToolName: tc.toolName,
			Args:     tc.arguments,
			FilePath: extractFilePath(tc.arguments),
			Message:  result.Output,
		}, hooks.HookRuntimeOptions{Cwd: a.WorkDir})
	}

	return toolExecResult{
		toolID:        tc.toolID,
		toolName:      tc.toolName,
		output:        result.Output,
		isError:       result.IsError,
		elapsed:       time.Since(start),
		contentBlocks: result.ContentBlocks,
	}
}

func formatToolArgs(args map[string]any) string {
	var parts []string
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		if len(s) > 80 {
			s = s[:80] + "…"
		}
		parts = append(parts, fmt.Sprintf("%s: %s", k, s))
	}
	return strings.Join(parts, ", ")
}

func (a *Agent) persistLastMessage(conv *conversation.Manager) {
	if a.WorkDir == "" || a.SessionID == "" {
		return
	}
	msgs := conv.GetMessages()
	if len(msgs) == 0 {
		return
	}
	session.SaveMessage(a.WorkDir, a.SessionID, session.FromConversation(msgs[len(msgs)-1]))
}
