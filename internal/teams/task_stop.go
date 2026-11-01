package teams

import (
	"context"
	"fmt"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

type TaskStopTool struct {
	TeamMgr     *TeamManager
	TaskManager tools.BackgroundTaskManager
	Delegated   bool
}

func (t *TaskStopTool) Name() string { return "TaskStop" }
func (t *TaskStopTool) Category() tools.ToolCategory {
	if t.Delegated {
		return tools.CategoryRead
	}
	return tools.CategoryCommand
}

func (t *TaskStopTool) Description() string {
	if t.Delegated {
		return "Stop one of this agent's own background tasks (Agent, Bash, PowerShell). Pass task_id."
	}
	return "Stop a running teammate or stop a background tool call early (subagent, Bash, PowerShell). Pass exactly one of teammate or task_id. Cancellation waits for cleanup; terminal background tasks leave the active UI while their results remain available through TaskOutput."
}
func (t *TaskStopTool) ForSubagent() tools.Tool {
	return &TaskStopTool{TeamMgr: t.TeamMgr, Delegated: true}
}
func (t *TaskStopTool) Schema() map[string]any {
	properties := map[string]any{"task_id": map[string]any{"type": "string", "description": "ID of a background task (Agent, Bash, PowerShell)"}}
	schema := map[string]any{"type": "object", "properties": properties}
	if t.Delegated {
		schema["required"] = []string{"task_id"}
		schema["additionalProperties"] = false
	} else {
		properties["teammate"] = map[string]any{"type": "string", "description": "Name of the teammate to stop, exactly as it appears in the from= field of a task-notification"}
		properties["team_name"] = map[string]any{"type": "string"}
	}
	return map[string]any{"name": t.Name(), "description": t.Description(), "input_schema": schema}
}

func (t *TaskStopTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	name, _ := args["teammate"].(string)
	id, _ := args["task_id"].(string)
	if (name == "") == (id == "") {
		return tools.ToolResult{Output: "Error: pass exactly one of teammate or task_id", IsError: true}
	}
	if id != "" {
		manager := tools.TaskManagerForContext(ctx, t.TaskManager)
		if manager == nil {
			return tools.ToolResult{Output: fmt.Sprintf("Error: background task '%s' not found", id), IsError: true}
		}
		task := manager.Get(id)
		if task == nil {
			return tools.ToolResult{Output: fmt.Sprintf("Error: background task '%s' not found", id), IsError: true}
		}
		if task.Status != "running" {
			select {
			case <-task.Done:
			case <-ctx.Done():
				return tools.ToolResult{Output: ctx.Err().Error(), IsError: true}
			}
			return tools.ToolResult{Output: fmt.Sprintf("Background task '%s' is %s, nothing to stop", id, task.Status)}
		}
		if _, err := manager.StopAndWait(ctx, id); err != nil {
			return tools.ToolResult{Output: err.Error(), IsError: true}
		}
		return tools.ToolResult{Output: fmt.Sprintf("Background task '%s' stopped.", id)}
	}
	if t.Delegated {
		return tools.ToolResult{Output: "Only the leader can stop teammates.", IsError: true}
	}
	if t.TeamMgr == nil {
		return tools.ToolResult{Output: "Error: team manager unavailable", IsError: true}
	}

	requestedTeam, _ := args["team_name"].(string)
	for _, teamName := range t.TeamMgr.ListTeams() {
		if requestedTeam != "" && requestedTeam != teamName {
			continue
		}
		team := t.TeamMgr.GetTeam(teamName)
		if team == nil {
			continue
		}
		member, ok := team.Members[name]
		if !ok {
			continue
		}
		if !member.Active {
			return tools.ToolResult{
				Output: fmt.Sprintf("Teammate '%s' in team '%s' is not running, nothing to stop", name, teamName),
			}
		}
		team.StopMember(name)
		return tools.ToolResult{
			Output: fmt.Sprintf("Teammate '%s' in team '%s' stopped.", name, teamName),
		}
	}

	return tools.ToolResult{
		Output:  fmt.Sprintf("Error: teammate '%s' not found. Known teammates: %s", name, t.knownMembers()),
		IsError: true,
	}
}

func (t *TaskStopTool) knownMembers() string {
	var names []string
	for _, teamName := range t.TeamMgr.ListTeams() {
		team := t.TeamMgr.GetTeam(teamName)
		if team == nil {
			continue
		}
		for memberName := range team.Members {
			names = append(names, memberName)
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
