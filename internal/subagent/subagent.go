package subagent

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

type SubAgentSpec struct {
	Name                 string
	Description          string
	Tools                []string
	DisallowedTools      []string
	SystemPromptOverride string
	MaxTurns             int
	Model                string

	PermissionMode string

	Background bool

	Isolation IsolationMode

	InitialPrompt string

	OmitYukinoMd bool

	Skills []string

	Memory AgentMemoryScope

	McpServers         []any
	RequiredMcpServers []string
	Hooks              any
	Effort             any
}

const planAgentSystemPrompt = `You are a software architect and planning specialist.

=== CRITICAL: READ-ONLY MODE - NO FILE MODIFICATIONS ===
You are STRICTLY PROHIBITED from creating, modifying, or deleting any files.
Your role is EXCLUSIVELY to explore code and design implementation plans.

## Your Process

1. **Understand Requirements**: Analyze the user's request carefully.

2. **Explore Thoroughly**:
   - Read files with ReadFile to understand current architecture
   - Use Grep to find patterns, function definitions, and references
   - Use Glob to discover file structure
   - Use Bash ONLY for read-only operations (ls, find, grep, cat, head, tail)
   - NEVER use Bash for: mkdir, touch, rm, cp, mv, git add/commit, npm install

3. **Design Solution**:
   - Create a concrete implementation approach
   - Consider trade-offs and explain your reasoning
   - Follow existing patterns in the codebase

4. **Detail the Plan**:
   - Provide step-by-step implementation strategy
   - Identify file dependencies and sequencing
   - Anticipate potential challenges

## Required Output
End your response with:

### Critical Files for Implementation
List the most critical files for implementing this change:
- path/to/file1 — reason
- path/to/file2 — reason`

var BuiltinSpecs = map[string]SubAgentSpec{
	"general-purpose": {
		Name:        "general-purpose",
		Description: "General-purpose agent for research and multi-step tasks",
		MaxTurns:    200,
	},
	"plan": {
		Name:                 "plan",
		Description:          "Software architect for designing implementation plans. Returns step-by-step plans, identifies critical files, and considers architectural trade-offs.",
		DisallowedTools:      []string{"EditFile", "WriteFile"},
		SystemPromptOverride: planAgentSystemPrompt,
		MaxTurns:             15,
	},
	"explore": {
		Name:            "explore",
		Description:     "Fast read-only search agent for locating code",
		DisallowedTools: []string{"EditFile", "WriteFile"},
		Model:           "haiku",
	},
}

func SpawnSubAgent(
	ctx context.Context,
	taskMgr *TaskManager,
	client llm.Client,
	registry *tools.Registry,
	protocol string,
	spec SubAgentSpec,
	taskPrompt string,
	parentChecker *permissions.Checker,
) string {
	subCtx, cancel := context.WithCancel(ctx)

	subRegistry := FilterToolsForAgent(registry, spec.Tools, spec.DisallowedTools, true)

	subAgent := agent.New(client, subRegistry, protocol)
	childTasks := configureBackgroundRuntime(subAgent)
	subAgent.Checker = deriveSubAgentChecker(parentChecker, spec.PermissionMode)
	if spec.MaxTurns > 0 {
		subAgent.MaxIterations = spec.MaxTurns
	} else {
		subAgent.MaxIterations = 200
	}

	taskID, err := taskMgr.Create(spec.Name+": "+truncate(taskPrompt, 50), func(id string) (string, error) {
		defer childTasks.StopAll(context.Background())
		conv := conversation.NewManager()
		if spec.SystemPromptOverride != "" {
			conv.AddSystemReminder(spec.SystemPromptOverride)
		}
		if spec.InitialPrompt != "" {
			conv.AddUserMessage(spec.InitialPrompt)
		}
		conv.AddUserMessage(taskPrompt)

		var output string
		ch := subAgent.Run(subCtx, conv)
		for ev := range ch {
			switch e := ev.(type) {
			case agent.StreamText:
				output += e.Text
			case agent.PermissionRequestEvent:
				e.ResponseCh <- agent.PermissionReply{Response: agent.PermDeny}
			case agent.ErrorEvent:
				return "", fmt.Errorf("%s", e.Message)
			case agent.LoopComplete:
			}
		}
		return output, nil
	}, cancel, tools.TaskOptions{})
	if err != nil {
		cancel()
		return ""
	}

	return taskID
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
