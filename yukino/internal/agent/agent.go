package agent

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/compact"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/file_history"
	"github.com/yukino-js/yukino/yukino/internal/goal"
	"github.com/yukino-js/yukino/yukino/internal/hooks"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/tools"
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
	TranscriptPath      string
	SessionID           string
	TaskManager         tools.BackgroundTaskManager
	DisableBackground   bool
	NotificationFn      func() []string
	InstructionsFn      func() string
	MemoryContentFn     func() string
	Instructions        string
	MemoryContent       string
	SkillSection        string
	SkillDeltaFn        func() string
	MemoryRecallCh      <-chan RecallResult
	ToolNameFilter      func(name string) bool
	CoordinatorActiveFn func() bool
	OnLoopComplete      func(conv *conversation.Manager)
	GoalManager         *goal.GoalManager
	ShouldContinueGoal  func() bool
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

func (a *Agent) CurrentCwd() string {
	if a.Checker != nil {
		if wt := a.Checker.WorktreeSession(); wt != nil {
			return wt.WorktreePath
		}
	}
	return a.WorkDir
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

func (a *Agent) emitHook(event hooks.EventName, message string, args map[string]any) {
	if a.Hooks == nil {
		return
	}
	for _, result := range a.Hooks.Fire(event, hooks.HookContext{
		Event:   event,
		Args:    args,
		Message: message,
	}, hooks.HookRuntimeOptions{Cwd: a.CurrentCwd()}) {
		a.Hooks.RecordNotification(result.Output)
	}
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

func (a *Agent) executeSingleTool(ctx context.Context, eventCh chan AgentEvent, tc toolCallInfo) (executionResult toolExecResult) {
	start := time.Now()
	fail := func(output string) toolExecResult {
		return toolExecResult{toolID: tc.toolID, toolName: tc.toolName, output: output, isError: true, elapsed: time.Since(start)}
	}
	defer func() {
		if value := recover(); value != nil {
			executionResult = fail(fmt.Sprintf("Error executing %s: %v", tc.toolName, value))
		}
	}()
	if tc.parseError != "" {
		return fail("Error: " + tc.parseError + ". The tool was not executed.")
	}
	if ctx.Err() != nil {
		return fail(tools.CommandInterruptedError)
	}
	if a.ToolNameFilter != nil && !a.ToolNameFilter(tc.toolName) {
		return fail(fmt.Sprintf("Tool '%s' is not available to this agent.", tc.toolName))
	}
	tool := a.Registry.Get(tc.toolName)
	if tool == nil {
		return fail(fmt.Sprintf("Error: unknown tool '%s'", tc.toolName))
	}
	type permissionCall struct {
		tool tools.Tool
		args map[string]any
	}
	calls := []permissionCall{{tool, tc.arguments}}
	if router, ok := tool.(*tools.McpCallTool); ok {
		tc.arguments = router.PrepareArguments(tc.arguments)
		calls[0].args = tc.arguments
		if target := router.ResolveTarget(tc.arguments); target != nil {
			if a.ToolNameFilter != nil && !a.ToolNameFilter(target.Name()) {
				return fail(fmt.Sprintf("Tool '%s' is not available to this agent.", target.Name()))
			}
			inner, _ := tc.arguments["arguments"].(map[string]any)
			calls = append(calls, permissionCall{target, inner})
		}
	}
	if a.Hooks != nil {
		for _, call := range calls {
			if rejected, reason := a.Hooks.FirePreToolHooks(call.tool.Name(), call.args, hooks.HookRuntimeOptions{Cwd: a.CurrentCwd(), Abort: ctx}); rejected {
				return fail("Rejected by hook: " + reason)
			}
		}
	}
	if a.Checker != nil {
		decisions := make([]permissions.Decision, len(calls))
		for i, call := range calls {
			decisions[i] = a.Checker.Check(call.tool, call.args)
		}
		if len(decisions) > 1 && decisions[0].Effect == permissions.Allow && decisions[0].Reason == "Permission rule: allow" && decisions[1].Effect == permissions.Ask && strings.HasPrefix(decisions[1].Reason, "Mode:") {
			decisions[1] = decisions[0]
		}
		decision := decisions[0]
		for _, candidate := range decisions {
			if candidate.Effect == permissions.Ask {
				decision = candidate
			}
		}
		for _, candidate := range decisions {
			if candidate.Effect == permissions.Deny {
				decision = candidate
				break
			}
		}
		if decision.Effect == permissions.Deny {
			return fail(BuildPermissionDeniedOutput(decision.Reason))
		}
		if decision.Effect == permissions.Ask {
			patterns := append([]string{}, a.Checker.RememberPatterns(tc.toolName, tc.arguments)...)
			decision.RememberPatterns = patterns
			response := make(chan PermissionReply, 1)
			request := PermissionRequestEvent{ToolName: tc.toolName, ToolID: tc.toolID, Args: tc.arguments, Decision: decision, Desc: permissions.DescribeToolAction(tc.toolName, tc.arguments), ResponseCh: response}
			select {
			case eventCh <- request:
			case <-ctx.Done():
				return fail(tools.CommandInterruptedError)
			}
			var reply PermissionReply
			select {
			case reply = <-response:
			case <-ctx.Done():
				return fail(tools.CommandInterruptedError)
			}
			if reply.Response == PermDeny {
				return fail(conversation.RejectedToolResultWithFeedback(reply.Feedback))
			}
			if ctx.Err() != nil {
				return fail(tools.CommandInterruptedError)
			}
			if reply.Response == PermAllowAlways {
				if err := a.Checker.AllowAlways(tc.toolName, tc.arguments, patterns); err != nil {
					return fail("Permission request failed: " + err.Error() + ". The tool was not executed.")
				}
			}
		}
	}
	approvedMode := permissions.PermissionMode("")
	if a.Checker != nil {
		approvedMode = a.Checker.Mode()
	}

	var planning *tools.PlanContext
	if a.Checker != nil {
		planning = &tools.PlanContext{Owner: a.Checker, IsPlanMode: func() bool { return a.Checker.Mode() == permissions.ModePlan }, EnterPlanMode: func() { a.Checker.EnterPlanMode() }, IsSubagent: a.Checker.IsSubagent(), IsTeammate: a.Checker.Teammate}
	}
	var worktrees tools.WorktreeRuntime
	if a.Checker != nil {
		worktrees = a.Checker
	}
	ctx = tools.WithExecutionContext(ctx, tools.ExecutionContext{Worktrees: worktrees, Plan: planning, RequestPermission: func(requestCtx context.Context, name, desc string) (int, string) {
		response := make(chan PermissionReply, 1)
		select {
		case eventCh <- PermissionRequestEvent{ToolName: name, Desc: desc, ResponseCh: response}:
		case <-requestCtx.Done():
			return int(PermDeny), ""
		}
		select {
		case reply := <-response:
			return int(reply.Response), reply.Feedback
		case <-requestCtx.Done():
			return int(PermDeny), ""
		}
	}, IsAgentRuntime: true, FileStateCache: a.FileStateCache, FileHistory: a.FileHistory, WorkDir: a.CurrentCwd(), SessionID: a.SessionID, ToolCallID: tc.toolID, TaskManager: a.TaskManager, DisableBackground: a.DisableBackground, CanReadSearchPath: func(path string) bool { return a.Checker == nil || a.Checker.CanReadSearchPath(path) }, CanTraverseSearchPath: func(path string) bool { return a.Checker == nil || a.Checker.CanTraverseSearchPath(path) }, CheckPermission: func(name string, args map[string]any) *tools.ToolResult {
		if a.Checker == nil {
			return nil
		}
		target := a.Registry.Get(name)
		if target == nil {
			return &tools.ToolResult{Output: fmt.Sprintf("Error: unknown tool '%s'", name), IsError: true}
		}
		decision := a.Checker.Check(target, args)
		if decision.Effect == permissions.Deny || decision.Effect == permissions.Ask && a.Checker.Mode() != approvedMode {
			return &tools.ToolResult{Output: fmt.Sprintf("Permission changed while waiting: %s. Request approval again before retrying.", decision.Reason), IsError: true}
		}
		return nil
	}})
	result := tool.Execute(ctx, tc.arguments)

	a.RecordRecentTool(tc.toolName)

	if !result.IsError && tc.toolName == "ReadFile" && len(result.ContentBlocks) == 0 {
		if path, _ := tc.arguments["file_path"].(string); path != "" {
			a.RecoveryState.RecordFileRead(path, result.Output)
		}
	}
	if a.Hooks != nil {
		for i := len(calls) - 1; i >= 0; i-- {
			call := calls[i]
			for _, hookResult := range a.Hooks.Fire(hooks.EventPostToolUse, hooks.HookContext{Event: hooks.EventPostToolUse, ToolName: call.tool.Name(), Args: call.args, FilePath: extractFilePath(call.args), Message: result.Output}, hooks.HookRuntimeOptions{Cwd: a.CurrentCwd(), Abort: ctx}) {
				a.Hooks.RecordNotification(hookResult.Output)
			}
		}
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

func (a *Agent) persistLastMessage(conv *conversation.Manager) {
	if a.TranscriptPath == "" && (a.WorkDir == "" || a.SessionID == "") {
		return
	}
	msgs := conv.GetMessages()
	if len(msgs) == 0 {
		return
	}
	record := session.FromConversation(msgs[len(msgs)-1])
	if a.TranscriptPath != "" {
		session.SaveTranscriptMessage(a.TranscriptPath, record)
	} else {
		session.SaveMessage(a.WorkDir, a.SessionID, record)
	}
}

func (a *Agent) hasToolSchema(schemas []map[string]any, name string) bool {
	for _, schema := range schemas {
		if tools.SchemaName(schema) == name {
			return true
		}
	}
	return false
}
