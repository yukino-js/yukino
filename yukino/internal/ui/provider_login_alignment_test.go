package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
)

func TestLoginFieldNavigationAndDefaultValues(t *testing.T) {
	m := New(nil, nil, nil, nil, 0, "", false)
	if m.loginForm.protocol != "anthropic" || m.loginForm.contextWindow == "" || m.loginForm.maxOutputTokens == "" {
		t.Fatal("startup form missing defaults")
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyTab}, {Type: tea.KeyDown}, {Type: tea.KeyShiftTab}, {Type: tea.KeyUp}} {
		before := m.loginCursor
		next, _ := m.handleLoginDialog(key)
		m = asModel(t, next)
		if m.loginCursor == before {
			t.Fatalf("%s did not move field", key.String())
		}
	}
	m.loginCursor = 0
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("hello")}, {Type: tea.KeySpace}, {Type: tea.KeyRunes, Runes: []rune("world")}} {
		next, _ := m.handleLoginDialog(key)
		m = asModel(t, next)
	}
	if m.loginForm.name != "hello world" {
		t.Fatalf("login spaces lost: %q", m.loginForm.name)
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}, {Type: tea.KeyRunes, Runes: []rune("\x1b[<0;1;1M")}} {
		next, _ := m.handleLoginDialog(key)
		m = asModel(t, next)
	}
	if m.loginForm.name != "hello world" {
		t.Fatal("terminal protocol entered login field")
	}
}

func TestLoginDiscoveryCancelsAndRejectsStaleResults(t *testing.T) {
	m := newChatModel(t)
	m.loginForm = newProviderLoginForm(nil)
	m.loginForm.baseURL = "https://example.invalid/v1"
	m.loginCursor = 3
	ctx, cancel := context.WithCancel(context.Background())
	m.loginDiscoveryCancel = cancel
	m.loginDiscoveryGen = 3
	cmd := m.loginMoveField(1)
	if m.loginCursor != 4 || ctx.Err() == nil || m.loginDiscoveryGen != 4 || cmd == nil {
		t.Fatal("leaving connection did not reset discovery")
	}
	m.handleLoginDiscoveryDone(loginDiscoveryDoneMsg{gen: 3, models: []llm.DiscoveredModel{{ID: "stale"}}})
	if m.loginForm.model != "" {
		t.Fatal("stale discovery changed model")
	}
	m.loginForm.name = "n"
	m.loginForm.apiKey = "k"
	m.loginForm.model = "m"
	m.loginForm.contextWindow = "bad"
	if _, err := config.ParseProviderLogin(m.loginInput()); err == nil {
		t.Fatal("invalid token text silently used defaults")
	}
	m.loginForm.contextWindow = "200000"
	no := false
	m.loginInitial = &config.ProviderConfig{Reasoning: &no, ThinkingLevelMap: map[string]config.LevelOverride{"off": {Effort: "none"}}}
	p, err := config.ParseProviderLogin(m.loginInput())
	if err != nil || p.Reasoning == nil || *p.Reasoning || p.ThinkingLevelMap["off"].Effort != "none" {
		t.Fatalf("provider metadata lost: %+v %v", p, err)
	}
}

func TestSubagentProgressRestartsAndKeepsEmptyTerminalOutput(t *testing.T) {
	m := newChatModel(t)
	m.applySubagentProgress(subagent.SubAgentProgress{ToolCallID: "call", Role: "explore", Status: "running"})
	m.applySubagentProgress(subagent.SubAgentProgress{ToolCallID: "call", Event: agent.ToolUseEvent{ToolID: "read", ToolName: "ReadFile"}})
	m.applySubagentProgress(subagent.SubAgentProgress{ToolCallID: "call", Event: agent.ToolUseEvent{ToolID: "read", ToolName: "Grep"}})
	if len(m.subagents["call"].active) != 1 {
		t.Fatal("duplicate tool IDs were not replaced")
	}
	m.applySubagentProgress(subagent.SubAgentProgress{ToolCallID: "call", Event: agent.TurnComplete{}})
	m.applySubagentProgress(subagent.SubAgentProgress{ToolCallID: "call", Role: "explore", Status: "running"})
	if m.subagents["call"].turns != 0 {
		t.Fatal("new tracking run retained turn count")
	}
	m.applySubagentProgress(subagent.SubAgentProgress{ToolCallID: "call", Status: "completed", Output: ""})
	card := m.decorateTool(toolBlockInfo{toolID: "call", toolName: "Agent", output: "stale"})
	if card.output != "" || !strings.Contains(card.progress, "0 turns") {
		t.Fatalf("terminal card %+v", card)
	}
}
