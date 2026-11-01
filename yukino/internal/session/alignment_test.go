package session

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/history"
	"github.com/yukino-js/yukino/yukino/internal/storage"
)

func TestResumePreservesMessageMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	id := "metadata"
	input, err := history.RecordInput(history.InputOptions{Cwd: cwd, SessionID: &id, Source: "sdk", Text: "request"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := SaveUserMessage(cwd, id, input, Message{ContentBlocks: []map[string]any{{"type": "text", "text": "expanded request"}}})
	if err != nil {
		t.Fatal(err)
	}
	assistant := conversation.Message{Role: "assistant", ThinkingBlocks: []conversation.ThinkingBlock{{Type: "thinking", Thinking: "thought", Signature: ""}, {Type: "redacted_thinking", Data: "opaque"}}, ToolUses: []conversation.ToolUseBlock{{ToolUseID: "call", ToolName: "ReadFile", ProviderItemID: "provider-call"}}}
	if err := SaveMessage(cwd, id, FromConversation(assistant)); err != nil {
		t.Fatal(err)
	}
	result := conversation.Message{Role: "user", ToolResults: []conversation.ToolResultBlock{{ToolUseID: "call", Content: "fallback", ContentBlocks: []map[string]any{{"type": "text", "text": "rich"}}}}}
	if err := SaveMessage(cwd, id, FromConversation(result)); err != nil {
		t.Fatal(err)
	}
	loaded := LoadSession(cwd, id)
	got := RebuildFromSession(loaded, false)
	if len(got) != 3 || got[0].UserInput == nil || *got[0].UserInput != *user.UserInput || !reflect.DeepEqual(got[0].ContentBlocks, user.ContentBlocks) || !reflect.DeepEqual(got[1].ThinkingBlocks, assistant.ThinkingBlocks) || got[1].ToolUses[0].ProviderItemID != "provider-call" || !reflect.DeepEqual(got[2].ToolResults, result.ToolResults) {
		t.Fatalf("metadata lost on resume: %+v", got)
	}
	data, err := os.ReadFile(SessionFilePath(cwd, id))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"ts":`) || !strings.Contains(string(data), `"timestamp":`) {
		t.Fatalf("incorrect persisted timestamp: %s", data)
	}
	foreign := input
	foreign.Cwd = t.TempDir()
	if _, err := SaveUserMessage(cwd, id, foreign, Message{Content: "wrong project"}); err == nil {
		t.Fatal("accepted input for another project")
	}
}

func TestResumeFallsBackToLastValidBoundary(t *testing.T) {
	valid, err := json.Marshal(CompactBoundary{Summary: "valid summary", Keep: []KeepMessage{{Role: "assistant", Content: "kept"}}})
	if err != nil {
		t.Fatal(err)
	}
	saved := []Message{{Role: "user", Content: "old"}, {Role: "system", Type: TypeCompactBoundary, Content: string(valid)}, {Role: "user", Content: "new"}, {Role: "system", Type: TypeCompactBoundary, Content: `{"summary":"","keep":[]}`}, {Role: "system", Type: TypeCompactBoundary, Content: `{"summary":"broken","keep":[{}]}`}}
	got := RebuildFromSession(saved, false)
	if len(got) != 3 || !strings.Contains(got[0].Content, "valid summary") || got[1].Content != "kept" || got[2].Content != "new" {
		t.Fatalf("bad boundary discarded recoverable history: %+v", got)
	}
}

func TestTruncateAndExpiryRemoveArtifacts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	id := "rewind"
	for _, content := range []string{"one", "two", "three"} {
		if err := SaveMessage(cwd, id, Message{Role: "user", Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	path := SessionFilePath(cwd, id)
	if count, exists, err := SessionLineCount(path); err != nil || !exists || count != 3 {
		t.Fatalf("line count = %d,%v,%v", count, exists, err)
	}
	if err := TruncateSessionLines(path, 2); err != nil {
		t.Fatal(err)
	}
	got := LoadSession(cwd, id)
	if len(got) != 2 || got[1].Content != "two" {
		t.Fatalf("rewind did not survive reload: %+v", got)
	}
	artifact, err := storage.SessionPath(id, "tool-results", "output.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.AppendPrivateFileSync(artifact, []byte("output")); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if CleanExpiredSessions(cwd) != 1 {
		t.Fatal("expired transcript was not removed")
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("expired artifact remains: %v", err)
	}
}
