package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

type countedTool struct {
	mockTool
	calls atomic.Int32
}

func (t *countedTool) Execute(context.Context, map[string]any) tools.ToolResult {
	t.calls.Add(1)
	return tools.ToolResult{Output: "executed"}
}

func TestInvalidOrUnavailableCallsNeverExecute(t *testing.T) {
	for _, test := range []struct {
		name, parseError, stopReason string
		filtered                     bool
	}{{"malformed", "invalid JSON", "tool_use", false}, {"truncated", "", "max_tokens", false}, {"filtered", "", "tool_use", true}} {
		t.Run(test.name, func(t *testing.T) {
			tool := &countedTool{mockTool: mockTool{name: "WriteFile"}}
			registry := tools.NewRegistry()
			registry.Register(tool)
			client := &mockClient{responses: [][]llm.StreamEvent{{llm.ToolCallStart{ToolID: "call", ToolName: "WriteFile"}, llm.ToolCallComplete{ToolID: "call", ToolName: "WriteFile", Arguments: map[string]any{}, ParseError: test.parseError}, llm.StreamEnd{StopReason: test.stopReason}}, {llm.TextDelta{Text: "done"}, llm.StreamEnd{StopReason: "end_turn"}}}}
			ag := New(client, registry, "anthropic")
			if test.filtered {
				ag.ToolNameFilter = func(string) bool { return false }
			}
			conv := conversation.NewManager()
			_, events := runConversationRound(ag, conv, "request")
			results := getToolResults(events)
			if tool.calls.Load() != 0 || len(results) != 1 || !results[0].IsError {
				t.Fatalf("invalid call executed: count=%d results=%+v", tool.calls.Load(), results)
			}
			calls := 0
			for _, event := range events {
				if _, ok := event.(ToolUseEvent); ok {
					calls++
				}
			}
			if calls != 1 {
				t.Fatalf("tool use emitted %d times", calls)
			}
		})
	}
}

func TestIncompleteProviderTurnPreservesPartialOutput(t *testing.T) {
	client := &mockClient{responses: [][]llm.StreamEvent{{llm.TextDelta{Text: "partial"}, llm.ThinkingComplete{Thinking: "thought", Signature: "signature"}}}}
	ag := New(client, tools.NewRegistry(), "anthropic")
	conv := conversation.NewManager()
	_, events := runConversationRound(ag, conv, "request")
	found := false
	for _, event := range events {
		if err, ok := event.(ErrorEvent); ok && strings.Contains(err.Message, "without a completion event") {
			found = true
		}
	}
	messages := conv.GetMessages()
	last := messages[len(messages)-1]
	if !found || last.Content != "partial" || len(last.ThinkingBlocks) != 1 {
		t.Fatalf("truncated stream treated as complete: events=%+v last=%+v", events, last)
	}
}

func TestPerTurnUsageAndOpaqueThinkingArePreserved(t *testing.T) {
	client := &mockClient{responses: [][]llm.StreamEvent{
		{llm.RedactedThinkingComplete{Data: "opaque"}, llm.ToolCallComplete{ToolID: "call", ToolName: "ReadFile"}, llm.StreamEnd{StopReason: "tool_use", Usage: llm.UsageInfo{InputTokens: 13, OutputTokens: 2, CacheReadTokens: 5}}},
		{llm.TextDelta{Text: "done"}, llm.StreamEnd{StopReason: "end_turn", Usage: llm.UsageInfo{InputTokens: 7, OutputTokens: 3}}},
	}}
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "ReadFile", result: "read"})
	conv := conversation.NewManager()
	_, events := runConversationRound(New(client, registry, "anthropic"), conv, "request")
	var usage []UsageEvent
	for _, event := range events {
		if value, ok := event.(UsageEvent); ok {
			usage = append(usage, value)
		}
	}
	if len(usage) != 2 || usage[0].InputTokens != 13 || usage[0].CacheReadTokens != 5 || usage[1].InputTokens != 7 {
		t.Fatalf("usage was accumulated or lost: %+v", usage)
	}
	found := false
	for _, message := range conv.GetMessages() {
		for _, block := range message.ThinkingBlocks {
			if block.Type == "redacted_thinking" && block.Data == "opaque" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("opaque thinking was dropped")
	}
}

func TestCancelledRunEmitsInterruptedCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := collectEvents(New(&mockClient{}, tools.NewRegistry(), "anthropic").Run(ctx, conversation.NewManager()))
	if len(events) != 1 {
		t.Fatalf("unexpected canceled events: %+v", events)
	}
	if complete, ok := events[0].(LoopComplete); !ok || complete.StopReason != "interrupted" {
		t.Fatalf("missing interrupted completion: %+v", events)
	}
}
