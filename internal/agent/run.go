package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/compact"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/hooks"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/plan_file"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/prompt"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tool_result"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

type runState struct {
	maxOutput        int
	escalated        bool
	outputRecoveries int
	transientRetries int
}

func (a *Agent) restoreContext(conv *conversation.Manager) {
	sections := []string{a.SkillSection}
	names := make([]string, 0, len(a.activeSkills))
	for name := range a.activeSkills {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sections = append(sections, "## Active skill: "+name+"\n"+a.activeSkills[name])
	}
	instructions, memory := a.Instructions, a.MemoryContent
	if a.InstructionsFn != nil {
		instructions = a.InstructionsFn()
	}
	if a.MemoryContentFn != nil {
		memory = a.MemoryContentFn()
	}
	conv.InjectLongTermMemory(instructions, memory, strings.Join(sections, "\n\n"))
}

func (a *Agent) Run(ctx context.Context, conv *conversation.Manager) <-chan AgentEvent {
	ch := make(chan AgentEvent, 32)
	go func() {
		defer close(ch)
		defer a.emitHook(hooks.EventSessionEnd, "", nil)
		a.restoreContext(conv)
		a.emitHook(hooks.EventSessionStart, "", nil)
		state := runState{maxOutput: a.MaxOutputTokens}
		if state.maxOutput <= 0 {
			state.maxOutput = config.DefaultMaxOutputTokens
		}
		for iteration := 1; ; iteration++ {
			if ctx.Err() != nil {
				ch <- LoopComplete{TotalTurns: iteration - 1, StopReason: "interrupted"}
				return
			}
			if a.MaxIterations > 0 && iteration > a.MaxIterations {
				ch <- ErrorEvent{Message: fmt.Sprintf("Agent reached maximum iterations (%d)", a.MaxIterations)}
				return
			}
			if !a.runTurn(ctx, ch, conv, iteration, &state) {
				return
			}
		}
	}()
	return ch
}

func (a *Agent) runTurn(ctx context.Context, ch chan AgentEvent, conv *conversation.Manager, iteration int, state *runState) bool {
	schemas := a.currentToolSchemas()
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		if name := tools.SchemaName(schema); name != "" {
			names = append(names, name)
		}
	}
	coordinating := a.CoordinatorActiveFn != nil && a.CoordinatorActiveFn()
	guidance := prompt.BuildToolGuidance(names, coordinating)
	if guidance == "" && conv.HasReminderContaining(prompt.ToolGuidanceMarker) {
		guidance = prompt.ToolGuidanceMarker + "\nNo tools are currently callable. Return findings or blockers without issuing tool calls."
	}
	if guidance != "" {
		conv.AddSystemReminderIfChanged(prompt.ToolGuidanceMarker, guidance)
	}
	if a.Checker != nil && a.Checker.Mode() == permissions.ModePlan {
		path, err := plan_file.GetOrCreatePlanPath(a.Checker)
		if err != nil {
			ch <- ErrorEvent{Message: err.Error()}
			return false
		}
		conv.AddSystemReminder(prompt.BuildPlanModeReminder(path, plan_file.PlanExists(a.Checker), iteration, prompt.PlanCapabilities{
			CanAskUser: a.hasToolSchema(schemas, "AskUserQuestion"), CanExitPlanMode: a.hasToolSchema(schemas, "ExitPlanMode"), CanSendMessage: a.hasToolSchema(schemas, "SendMessage"), CanWriteFile: a.hasToolSchema(schemas, "WriteFile"), CanEditFile: a.hasToolSchema(schemas, "EditFile"), CanDelegate: a.hasToolSchema(schemas, "Agent"), IsCoordinator: coordinating, IsSubagent: a.Checker.IsSubagent(), IsTeammate: a.Checker.Teammate, ReadOnly: a.Checker.ReadOnly, RequiresLeaderPlanApproval: a.Checker.RequiresLeaderPlanApproval,
		}))
	}
	if coordinating {
		conv.AddSystemReminder(prompt.CoordinatorReminder(iteration))
	}
	var deferred []string
	for _, name := range a.Registry.GetDeferredToolNames() {
		if a.ToolNameFilter == nil || a.ToolNameFilter(name) {
			deferred = append(deferred, name)
		}
	}
	deferredGuidance := prompt.BuildDeferredToolGuidance(deferred, names, a.Registry.McpLoadingMode == tools.McpLoadingDispatch)
	if deferredGuidance == "" && conv.HasReminderContaining(prompt.DeferredGuidanceMarker) {
		deferredGuidance = prompt.DeferredGuidanceMarker + "\nNo deferred tools can currently be discovered and invoked. Do not use earlier deferred-tool lists."
	}
	if deferredGuidance != "" {
		conv.AddSystemReminderIfChanged(prompt.DeferredGuidanceMarker, deferredGuidance)
	}
	drainHooks := func() {
		if a.Hooks != nil {
			for _, note := range a.Hooks.DrainNotifications() {
				conv.AddSystemReminder(note)
				a.persistLastMessage(conv)
			}
		}
	}
	drainHooks()
	if a.NotificationFn != nil {
		for _, note := range a.NotificationFn() {
			conv.AddSystemReminder(note)
			a.persistLastMessage(conv)
		}
	}
	if a.SkillDeltaFn != nil {
		if delta := a.SkillDeltaFn(); delta != "" {
			conv.AddSystemReminder("The following skills became available:\n" + delta)
		}
	}
	a.emitHook(hooks.EventTurnStart, "", nil)
	defer a.emitHook(hooks.EventTurnEnd, "", nil)
	a.emitHook(hooks.EventPreSend, "", nil)
	drainHooks()
	if msg, err := compact.ManageContext(ctx, conv, a.Client, a.WorkDir, a.SessionID, a.ContextWindow, state.maxOutput, &a.compactTracking, a.RecoveryState, schemas); err == nil && msg != "" {
		ch <- CompactEvent{Message: msg}
		a.restoreContext(conv)
		if guidance != "" {
			conv.AddSystemReminderIfChanged(prompt.ToolGuidanceMarker, guidance)
		}
		if deferredGuidance != "" {
			conv.AddSystemReminderIfChanged(prompt.DeferredGuidanceMarker, deferredGuidance)
		}
	}
	if ctx.Err() != nil {
		ch <- LoopComplete{TotalTurns: iteration, StopReason: "interrupted"}
		return false
	}
	streamCtx := ctx
	if state.escalated || a.MaxOutputTokens > 0 {
		streamCtx = llm.WithMaxOutputTokens(ctx, state.maxOutput)
	}
	events, errs := a.Client.Stream(streamCtx, conv, schemas)
	var text strings.Builder
	var calls []llm.ToolCallComplete
	var thinking []conversation.ThinkingBlock
	var usage llm.UsageInfo
	var streamErr error
	receivedEnd, receivedThinking := false, false
	stopReason := "end_turn"
	for events != nil || errs != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if ctx.Err() != nil {
				continue
			}
			switch e := event.(type) {
			case llm.TextDelta:
				text.WriteString(e.Text)
				ch <- StreamText{Text: e.Text}
			case llm.ThinkingDelta:
				receivedThinking = true
				ch <- ThinkingText{Text: e.Text}
			case llm.ThinkingComplete:
				thinking = append(thinking, conversation.ThinkingBlock{Type: "thinking", Thinking: e.Thinking, Signature: e.Signature})
				ch <- ThinkingComplete{Thinking: e.Thinking, Signature: e.Signature}
			case llm.RedactedThinkingComplete:
				thinking = append(thinking, conversation.ThinkingBlock{Type: "redacted_thinking", Data: e.Data})
			case llm.ToolCallComplete:
				calls = append(calls, e)
				ch <- ToolUseEvent{ToolID: e.ToolID, ToolName: e.ToolName, Args: e.Arguments}
			case llm.StreamEnd:
				receivedEnd = true
				stopReason = e.StopReason
				usage = e.Usage
				ch <- UsageEvent{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadTokens: usage.CacheReadTokens, CacheCreationTokens: usage.CacheCreationTokens}
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
			} else if err != nil {
				streamErr = err
			}
		}
	}
	persistPartial := func() {
		if text.Len() > 0 || len(thinking) > 0 {
			conv.AddAssistantFull(text.String(), thinking, nil)
			a.persistLastMessage(conv)
		}
	}
	if ctx.Err() != nil {
		persistPartial()
		ch <- LoopComplete{TotalTurns: iteration, StopReason: "interrupted"}
		return false
	}
	if streamErr == nil && !receivedEnd {
		streamErr = &llm.LLMError{Message: "Provider stream ended without a completion event"}
	}
	if streamErr != nil {
		var tooLong *llm.ContextTooLongError
		if errors.As(streamErr, &tooLong) {
			if msg, err := compact.ForceCompact(ctx, conv, a.Client, a.WorkDir, a.SessionID, a.ContextWindow, a.RecoveryState, schemas); err == nil && msg != "" {
				conv.ClearUsageAnchor()
				a.restoreContext(conv)
				ch <- CompactEvent{Message: "Auto-compacted due to context length: " + msg}
				return true
			}
		}
		if delay, retry := llm.LLMRetryDelay(streamErr, state.transientRetries); retry && text.Len() == 0 && !receivedThinking && len(thinking) == 0 && len(calls) == 0 {
			state.transientRetries++
			reason := "temporary provider failure"
			var rate *llm.RateLimitError
			if errors.As(streamErr, &rate) {
				reason = "rate limited"
			}
			ch <- RetryEvent{Reason: reason, Wait: delay}
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
				return true
			case <-ctx.Done():
				ch <- LoopComplete{TotalTurns: iteration, StopReason: "interrupted"}
				return false
			}
		}
		persistPartial()
		ch <- ErrorEvent{Message: streamErr.Error()}
		return false
	}
	state.transientRetries = 0
	a.emitHook(hooks.EventPostReceive, text.String(), nil)
	anchor := func() {
		conv.RecordUsageAnchor(usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheCreationTokens)
	}
	if stopReason == "max_tokens" && len(calls) == 0 {
		ceiling := min(maxTokensCeiling, a.ContextWindow)
		recovery := ""
		reason := ""
		if !state.escalated && state.maxOutput < ceiling {
			state.maxOutput = ceiling
			state.escalated = true
			recovery = "Output token limit hit. Resume directly from where you stopped. Do not apologize or repeat previous content. Pick up mid-thought if needed."
			reason = "max_tokens escalation"
		} else if state.outputRecoveries < maxOutputTokensRecoveries {
			state.outputRecoveries++
			recovery = "Output token limit hit. Resume directly from where you stopped. Break remaining work into smaller pieces."
			reason = fmt.Sprintf("max_tokens recovery %d/%d", state.outputRecoveries, maxOutputTokensRecoveries)
		}
		if recovery != "" {
			if text.Len() > 0 || len(thinking) > 0 {
				persistPartial()
				anchor()
				conv.AddUserMessage(recovery)
				a.persistLastMessage(conv)
			}
			ch <- RetryEvent{Reason: reason}
			return true
		}
	} else {
		state.outputRecoveries = 0
	}
	executor := NewStreamingExecutor(a.Registry, ch)
	var uses []conversation.ToolUseBlock
	for _, call := range calls {
		if stopReason == "max_tokens" {
			call.ParseError = "The response hit the output token limit, so tool arguments may be truncated. Re-issue the call with complete arguments"
		}
		uses = append(uses, conversation.ToolUseBlock{ToolUseID: call.ToolID, ToolName: call.ToolName, Arguments: call.Arguments, ParseError: call.ParseError, ProviderItemID: call.ProviderItemID})
		executor.Submit(toolCallInfo{toolID: call.ToolID, toolName: call.ToolName, arguments: call.Arguments, parseError: call.ParseError})
	}
	if text.Len() > 0 || len(thinking) > 0 || len(uses) > 0 {
		conv.AddAssistantFull(text.String(), thinking, uses)
		a.persistLastMessage(conv)
	}
	anchor()
	if len(calls) == 0 {
		ch <- TurnComplete{Turn: iteration}
		if a.FileHistory != nil {
			summary := []rune(text.String())
			if len(summary) > 60 {
				summary = append(summary[:60], []rune("...")...)
			}
			a.FileHistory.MakeSnapshot(conv.Len(), string(summary))
		}
		ch <- LoopComplete{TotalTurns: iteration, StopReason: stopReason}
		if a.OnLoopComplete != nil {
			go func() { defer func() { _ = recover() }(); a.OnLoopComplete(conv) }()
		}
		return false
	}
	results := executor.ExecuteAll(ctx, a)
	exempt := map[string]bool{}
	for _, call := range calls {
		if tool_result.IsSpillReadback(call.ToolName, call.Arguments, a.WorkDir, a.SessionID) {
			exempt[call.ToolID] = true
		}
	}
	var toolResults []conversation.ToolResultBlock
	planReady := false
	for _, result := range results {
		record := conversation.ToolResultBlock{ToolUseID: result.toolID, Content: result.output, IsError: result.isError, ContentBlocks: result.contentBlocks}
		if utf8.RuneCountInString(record.Content) > tools.MaxOutputChars && !exempt[result.toolID] {
			replacement := tool_result.PersistLargeResult(a.SessionID, result.toolID, record.Content)
			if replacement != record.Content {
				tool_result.ReplaceToolResultContent(&record, replacement)
			}
			exempt[result.toolID] = true
		}
		toolResults = append(toolResults, record)
		if !result.isError && (result.toolName == "EnterPlanMode" || result.toolName == "ExitPlanMode") {
			planReady = true
		}
	}
	tool_result.ApplyBudget(toolResults, a.SessionID, exempt)
	conv.AddToolResultsMessage(toolResults)
	a.persistLastMessage(conv)
	ch <- TurnComplete{Turn: iteration}
	if ctx.Err() != nil {
		ch <- LoopComplete{TotalTurns: iteration, StopReason: "interrupted"}
		return false
	}
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
	if planReady {
		ch <- LoopComplete{TotalTurns: iteration, PlanReady: true, StopReason: "end_turn"}
		return false
	}
	return true
}
