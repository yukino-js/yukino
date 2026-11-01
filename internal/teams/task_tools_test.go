package teams

import (
	"context"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/todo"
)

func newTestTeamManager(t *testing.T) *TeamManager {
	t.Helper()
	useTempHome(t)
	GetNameRegistry().Clear()
	t.Cleanup(func() { GetNameRegistry().Clear() })
	return NewTeamManager(t.TempDir())
}
func TestTeamTodoWriteFlowAndAtomicAssignment(t *testing.T) {
	mgr := newTestTeamManager(t)
	team := createTestTeam(t, mgr, "myteam", ModeInProcess)
	team.AddMember("alice", nil, nil, "")
	tool := &TeamTodoWriteTool{TeamMgr: mgr, TeamName: "myteam"}
	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{"todos": []any{map[string]any{"subject": "assigned", "owner": "alice"}, map[string]any{"subject": "invalid", "owner": "ghost"}}})
	if !result.IsError {
		t.Fatal(result)
	}
	tasks, err := mgr.GetTaskStore("myteam").ListTasks("", "")
	if err != nil || len(tasks) != 0 {
		t.Fatal(tasks, err)
	}
	messages, err := team.MailBox.ReadUnread("alice")
	if err != nil || len(messages) != 0 {
		t.Fatal(messages, err)
	}
	result = tool.Execute(ctx, map[string]any{"todos": []any{map[string]any{"subject": "assigned", "owner": "alice", "priority": "high"}, map[string]any{"subject": "other"}}})
	if result.IsError || !strings.Contains(result.Output, `"boardId": "team:myteam"`) || !strings.Contains(result.Output, `"createdBy": "leader"`) || !strings.Contains(result.Output, "DONE 0/2") {
		t.Fatal(result)
	}
	messages, err = team.MailBox.ReadUnread("alice")
	if err != nil || len(messages) != 1 || !strings.Contains(messages[0].Text, "TodoWrite") {
		t.Fatal(messages, err)
	}
	result = tool.Execute(ctx, map[string]any{"todos": []any{map[string]any{"id": "1", "status": "completed"}}})
	if result.IsError || !strings.Contains(result.Output, "DONE 1/2") {
		t.Fatal(result)
	}
	scoped := &TeamTodoWriteTool{TeamMgr: mgr, TeamName: "myteam", AgentName: "alice"}
	if result := scoped.Execute(ctx, map[string]any{"team_name": "other"}); !result.IsError {
		t.Fatal(result)
	}
	if result := tool.Execute(ctx, map[string]any{"todos": []any{map[string]any{"id": "42", "status": "completed"}}}); !result.IsError {
		t.Fatal(result)
	}
}
func TestTeamTodoWriteFallbackAndAmbiguousTeam(t *testing.T) {
	mgr := newTestTeamManager(t)
	list, _ := todo.NewTaskList(nil)
	tool := &TeamTodoWriteTool{TeamMgr: mgr, Fallback: list}
	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{"todos": []any{map[string]any{"subject": "private"}}})
	if result.IsError || !strings.Contains(result.Output, `"boardId": "private:`) {
		t.Fatal(result)
	}
	createTestTeam(t, mgr, "one", ModeInProcess)
	createTestTeam(t, mgr, "two", ModeInProcess)
	if result := tool.Execute(ctx, map[string]any{}); !result.IsError || !strings.Contains(result.Output, "Multiple teams") {
		t.Fatal(result)
	}
	result = tool.Execute(ctx, map[string]any{"team_name": "two", "todos": []any{map[string]any{"subject": "team"}}})
	if result.IsError || !strings.Contains(result.Output, `"boardId": "team:two"`) {
		t.Fatal(result)
	}
}
func TestDeleteTeamUnregistersMembers(t *testing.T) {
	mgr := newTestTeamManager(t)
	team := createTestTeam(t, mgr, "myteam", ModeInProcess)
	team.Members["alice"] = &Member{Name: "alice"}
	GetNameRegistry().Register("alice", "alice")
	mgr.DeleteTeam("myteam")
	if mgr.GetTeam("myteam") != nil || GetNameRegistry().Resolve("alice") != "" {
		t.Fatal("team or member registration retained")
	}
}

func createTestTeam(t *testing.T, manager *TeamManager, name string, mode TeamMode) *Team {
	t.Helper()
	team, err := manager.CreateTeam(name, mode)
	if err != nil {
		t.Fatal(err)
	}
	return team
}
func createTestTeamFull(t *testing.T, manager *TeamManager, name string, mode TeamMode, leader, description string) *Team {
	t.Helper()
	team, err := manager.CreateTeamFull(name, mode, leader, description)
	if err != nil {
		t.Fatal(err)
	}
	return team
}
