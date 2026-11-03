package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/storage"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

type writeMockTool struct {
	executed bool
}

func (t *writeMockTool) Name() string                 { return "WriteFile" }
func (t *writeMockTool) Description() string          { return "mock write" }
func (t *writeMockTool) Category() tools.ToolCategory { return tools.CategoryWrite }
func (t *writeMockTool) Schema() map[string]any {
	return map[string]any{
		"name": "WriteFile", "description": "mock",
		"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
	}
}
func (t *writeMockTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	t.executed = true
	return tools.ToolResult{Output: "written"}
}

func newAskAgent(t *testing.T, client llm.Client, tool *writeMockTool) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	reg := tools.NewRegistry()
	reg.Register(tool)
	ag := New(client, reg, "anthropic")
	ag.Checker = permissions.NewChecker(permissions.NewPathSandbox(dir, true), permissions.NewRuleEngine(dir), permissions.ModeDefault)
	return ag, filepath.Join(dir, "a.txt")
}

func writeCallResponses(path string) [][]llm.StreamEvent {
	return [][]llm.StreamEvent{
		{
			llm.ToolCallStart{ToolName: "WriteFile", ToolID: "w1"},
			llm.ToolCallComplete{ToolID: "w1", ToolName: "WriteFile", Arguments: map[string]any{"file_path": path, "content": "x"}},
			llm.StreamEnd{StopReason: "tool_use"},
		},
		{
			llm.TextDelta{Text: "ok"},
			llm.StreamEnd{StopReason: "end_turn"},
		},
	}
}

func TestPermissionDenyWithFeedbackReachesModel(t *testing.T) {
	tool := &writeMockTool{}
	client := &mockClient{}
	ag, path := newAskAgent(t, client, tool)
	client.responses = writeCallResponses(path)

	conv := conversation.NewManager()
	conv.AddUserMessage("write it")
	asked := false
	var results []ToolResultEvent
	for ev := range ag.Run(context.Background(), conv) {
		switch e := ev.(type) {
		case PermissionRequestEvent:
			asked = true
			e.ResponseCh <- PermissionReply{Response: PermDeny, Feedback: "  move it to backup/ instead  "}
		case ToolResultEvent:
			results = append(results, e)
		}
	}

	if !asked {
		t.Fatal("expected a permission request for a write tool in default mode")
	}
	if tool.executed {
		t.Error("tool must not run after the user denies it")
	}
	if len(results) != 1 || !results[0].IsError {
		t.Fatalf("expected one error tool result, got %+v", results)
	}
	want := conversation.RejectedToolResult + " To tell you how to proceed, the user said:\nmove it to backup/ instead"
	if results[0].Output != want {
		t.Errorf("tool result = %q, want %q", results[0].Output, want)
	}

	found := false
	for _, m := range conv.GetMessages() {
		for _, tr := range m.ToolResults {
			if strings.Contains(tr.Content, "move it to backup/ instead") {
				found = true
			}
		}
	}
	if !found {
		t.Error("feedback should be recorded in the conversation's tool_result")
	}
}

func TestPermissionDenyWithoutFeedbackKeepsPlainRejection(t *testing.T) {
	tool := &writeMockTool{}
	client := &mockClient{}
	ag, path := newAskAgent(t, client, tool)
	client.responses = writeCallResponses(path)

	conv := conversation.NewManager()
	conv.AddUserMessage("write it")
	var output string
	for ev := range ag.Run(context.Background(), conv) {
		switch e := ev.(type) {
		case PermissionRequestEvent:
			e.ResponseCh <- PermissionReply{Response: PermDeny}
		case ToolResultEvent:
			output = e.Output
		}
	}
	if output != conversation.RejectedToolResult {
		t.Errorf("tool result = %q, want plain rejection", output)
	}
}

func TestPermissionWaitReturnsWhenTurnCancelled(t *testing.T) {
	tool := &writeMockTool{}
	client := &mockClient{}
	ag, path := newAskAgent(t, client, tool)
	client.responses = writeCallResponses(path)

	conv := conversation.NewManager()
	conv.AddUserMessage("write it")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ag.Run(ctx, conv) {
			if _, ok := ev.(PermissionRequestEvent); ok {
				cancel()
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("agent stayed blocked on the permission reply after the turn was cancelled")
	}
	if tool.executed {
		t.Error("tool must not run when the turn is cancelled during confirmation")
	}
}

type commandMockTool struct {
	executed bool
}

func (t *commandMockTool) Name() string                 { return "CustomCommand" }
func (t *commandMockTool) Description() string          { return "mock command" }
func (t *commandMockTool) Category() tools.ToolCategory { return tools.CategoryCommand }
func (t *commandMockTool) Schema() map[string]any {
	return map[string]any{
		"name": "CustomCommand", "description": "mock",
		"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
	}
}
func (t *commandMockTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	t.executed = true
	return tools.ToolResult{Output: "done"}
}

func TestAllowAlwaysWithoutRememberScopesDoesNotPersist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tool := &commandMockTool{}
	client := &mockClient{}
	dir := t.TempDir()
	reg := tools.NewRegistry()
	reg.Register(tool)
	ag := New(client, reg, "anthropic")
	ag.Checker = permissions.NewChecker(permissions.NewPathSandbox(dir, true), permissions.NewRuleEngine(dir), permissions.ModeDefault)
	client.responses = [][]llm.StreamEvent{
		{
			llm.ToolCallStart{ToolName: "CustomCommand", ToolID: "c1"},
			llm.ToolCallComplete{ToolID: "c1", ToolName: "CustomCommand", Arguments: map[string]any{}},
			llm.StreamEnd{StopReason: "tool_use"},
		},
		{
			llm.TextDelta{Text: "ok"},
			llm.StreamEnd{StopReason: "end_turn"},
		},
	}

	conv := conversation.NewManager()
	conv.AddUserMessage("run it")
	var request *PermissionRequestEvent
	for ev := range ag.Run(context.Background(), conv) {
		if e, ok := ev.(PermissionRequestEvent); ok {
			request = &e
			e.ResponseCh <- PermissionReply{Response: PermAllowAlways}
		}
	}
	if request == nil || len(request.Decision.RememberPatterns) != 0 {
		t.Fatalf("expected a permission request without remember scopes, got %+v", request)
	}
	if !tool.executed {
		t.Fatal("tool must run after allowAlways is downgraded to allow")
	}
	rulesPath, err := storage.ProjectPath(dir, permissions.PermissionsRulesFilename)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(rulesPath); !os.IsNotExist(statErr) {
		t.Fatalf("allowAlways without scopes must not persist rules: %v", statErr)
	}
}
