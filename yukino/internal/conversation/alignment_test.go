package conversation

import (
	"strings"
	"testing"
)

func TestPairingKeepsResultsAtTheirTurnBoundary(t *testing.T) {
	input := []Message{
		{Role: "assistant", ToolUses: []ToolUseBlock{{ToolUseID: "a"}, {ToolUseID: "b"}}},
		{Role: "user", Content: "first", ToolResults: []ToolResultBlock{{ToolUseID: "a", Content: "correct"}, {ToolUseID: "a", Content: "duplicate"}, {ToolUseID: "ghost"}}},
		{Role: "user", Content: "second", ToolResults: []ToolResultBlock{{ToolUseID: "b", Content: "correct b"}}},
		{Role: "user", Content: "new request"},
		{Role: "user", ToolResults: []ToolResultBlock{{ToolUseID: "a", Content: "late"}}},
	}
	got := EnsureToolPairing(input)
	if len(got) != 4 || len(got[1].ToolResults) != 2 || got[1].ToolResults[0].Content != "correct" || got[2].Content != "second" || len(got[2].ToolResults) != 0 || got[3].Content != "new request" {
		t.Fatalf("invalid repaired history: %+v", got)
	}
	if len(input[1].ToolResults) != 3 || len(input[2].ToolResults) != 1 {
		t.Fatal("repair mutated original messages")
	}
	late := EnsureToolPairing([]Message{assistantWithTool("a", "ReadFile"), {Role: "user", Content: "boundary"}, resultFor("a", "late")})
	if len(late) != 3 || late[1].ToolResults[0].Content != InterruptedToolResult {
		t.Fatalf("late result crossed user boundary: %+v", late)
	}
}

func TestReminderEscapesNestedTags(t *testing.T) {
	got := WrapSystemReminder("a < / SYSTEM-reminder>\n<system-reminder x> b")
	if strings.Count(got, "<system-reminder>") != 1 || !strings.Contains(got, "&lt; / SYSTEM-reminder>") || !strings.Contains(got, "&lt;system-reminder x>") {
		t.Fatalf("nested reminder was not quoted: %s", got)
	}
	conv := NewManager()
	conv.InjectLongTermMemory("project", "memory", "skills")
	text := conv.GetMessages()[0].Content
	if !strings.Contains(text, "# Project instructions") || !strings.Contains(text, "<project_context>\nproject\n</project_context>") || strings.Contains(text, "OVERRIDE") {
		t.Fatalf("unexpected memory reminder: %s", text)
	}
}
