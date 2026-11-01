package teams

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/todo"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

const teamNameParamDescription = "Target team name returned by TeamCreate or TeamList. Required when multiple teams match; omitted when only one team matches. Teammates are restricted to their own team."
const teamTodoWriteDescription = "Use team_name to select a team's TODO board when multiple teams exist. Teammates always use their own team's board."

type teamTaskBoard struct {
	manager   *TeamManager
	teamName  string
	agentName string
	fallback  todo.Board
}

func (b *teamTaskBoard) board() (*Team, error) {
	team, err := b.manager.ResolveTeam(b.teamName)
	if err != nil {
		return nil, err
	}
	if team == nil && b.fallback == nil {
		return nil, fmt.Errorf("No active team task board")
	}
	return team, nil
}
func (b *teamTaskBoard) BoardID() string {
	team, err := b.board()
	if err != nil {
		return ""
	}
	if team == nil {
		return b.fallback.BoardID()
	}
	return "team:" + team.Name
}
func (b *teamTaskBoard) Write(input map[string]any) ([]*todo.Task, error) {
	team, err := b.board()
	if err != nil {
		return nil, err
	}
	if team == nil {
		return b.fallback.Write(input)
	}
	actor := b.agentName
	if actor == LeadName {
		actor = ""
	}
	tasks, err := b.manager.GetTaskStore(team.Name).Write(input, todo.WriteOptions{Actor: actor, CreateTask: func(task *todo.Task) { name := b.agentName; task.CreatedBy = &name }, ValidateOwner: func(name string) error {
		if name == LeadName {
			return nil
		}
		team.mu.Lock()
		member := team.Members[name]
		available := member != nil && (member.Active || member.Progress == nil)
		team.mu.Unlock()
		if !available {
			return fmt.Errorf("Teammate '%s' is not available for assignment", name)
		}
		return nil
	}})
	if err != nil {
		return nil, err
	}
	parsed, _ := todo.ParseWrite(input)
	newCount := 0
	for _, edit := range parsed.Todos {
		if edit["id"] == nil {
			newCount++
		}
	}
	newIndex := len(tasks) - newCount
	for _, edit := range parsed.Todos {
		id, _ := edit["id"].(string)
		if id == "" {
			id = tasks[newIndex].ID
			newIndex++
		}
		owner, _ := edit["owner"].(string)
		if owner == "" || owner == LeadName || owner == b.agentName {
			continue
		}
		for _, task := range tasks {
			if task.ID == id && task.Owner == owner {
				message := fmt.Sprintf("Task assignment: #%s %s\n%s\nUse TodoWrite({}) to inspect dependencies, then TodoWrite({todos:[{id:\"%s\",status:\"in_progress\"}]}) to claim it before starting.", task.ID, task.Subject, task.Description, task.ID)
				if err := team.MailBox.Send(owner, NewFileMailMessage(b.agentName, message)); err != nil {
					return nil, err
				}
			}
		}
	}
	return tasks, nil
}

type TeamTodoWriteTool struct {
	TeamMgr   *TeamManager
	TeamName  string
	AgentName string
	Fallback  todo.Board
}

func (t *TeamTodoWriteTool) Name() string { return "TodoWrite" }
func (t *TeamTodoWriteTool) Description() string {
	return todo.WriteDescription + " " + teamTodoWriteDescription
}
func (t *TeamTodoWriteTool) Category() tools.ToolCategory          { return tools.CategoryRead }
func (t *TeamTodoWriteTool) IsConcurrencySafe(map[string]any) bool { return true }
func (t *TeamTodoWriteTool) tool(name string) *todo.TodoWriteTool {
	actor := t.AgentName
	if actor == "" {
		actor = LeadName
	}
	return &todo.TodoWriteTool{Board: &teamTaskBoard{manager: t.TeamMgr, teamName: name, agentName: actor, fallback: t.Fallback}}
}
func (t *TeamTodoWriteTool) Schema() map[string]any {
	schema := t.tool(t.TeamName).Schema()
	schema["description"] = t.Description()
	schema["input_schema"].(map[string]any)["properties"].(map[string]any)["team_name"] = map[string]any{"type": "string", "description": teamNameParamDescription}
	return schema
}
func (t *TeamTodoWriteTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	name, _ := args["team_name"].(string)
	if name == "" {
		name = t.TeamName
	}
	if t.TeamName != "" && sanitizeTeamName(name) != sanitizeTeamName(t.TeamName) {
		return tools.ToolResult{Output: "Teammates can only access their own team.", IsError: true}
	}
	input := map[string]any{}
	for key, value := range args {
		if key != "team_name" {
			input[key] = value
		}
	}
	return t.tool(name).Execute(ctx, input)
}
func (t *TeamTodoWriteTool) ForSubagent() tools.Tool {
	list, _ := todo.NewTaskList(nil)
	return &todo.TodoWriteTool{Board: list}
}
func (tm *TeamManager) ResolveTeam(name string) (*Team, error) {
	if name != "" {
		team := tm.GetTeam(name)
		if team == nil {
			return nil, fmt.Errorf("Team '%s' not found", name)
		}
		return team, nil
	}
	names := tm.ListTeams()
	if len(names) > 1 {
		return nil, fmt.Errorf("Multiple teams match. Specify team_name; use TeamList to see available teams.")
	}
	if len(names) == 0 {
		return nil, nil
	}
	return tm.GetTeam(names[0]), nil
}
