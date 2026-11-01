package ui

import (
	"context"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/history"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
)

func (m Model) sendMessage(text string) (tea.Model, tea.Cmd) {
	m.refreshSkillsIfNeeded()
	wd, _ := os.Getwd()
	_, _ = history.RecordInput(history.InputOptions{Cwd: wd, SessionID: &m.sessionID, Source: "cli", Text: text})
	m.historyEntries = append(m.historyEntries, text)
	m.historyIndex = 0
	m.historyDraft = ""
	session.SaveMessage(wd, m.sessionID, session.Message{Role: "user", Content: text, Ts: time.Now().Unix()})

	m.streaming = true
	m.thinking = true
	m.thinkingStart = time.Now()
	m.thinkingDone = 0
	m.atMenuOpen = false
	m.atMatches = nil
	m.textarea.Reset()
	m.textarea.SetHeight(1)

	expanded := conversation.ExpandAtRefs(text, wd)

	m.chatMessages = append(m.chatMessages, chatMessage{role: "user", content: text})
	userLine := promptStyle.Render("❯ ") + lipgloss.NewStyle().Foreground(brightText).Bold(true).Render(text)
	m.committedUpTo = len(m.chatMessages)
	m.conversation.AddUserMessage(expanded)

	if m.mcpInstructions != "" && !m.mcpInstructionsOK {
		m.conversation.AddSystemReminder(m.mcpInstructions)
		m.mcpInstructionsOK = true
	}

	prefetchCh := m.prefetchRelevantMemories(expanded)

	m.streamBuf = ""
	m.toolBlocks = nil
	m.userScrolled = false

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelStream = cancel

	conv := m.conversation
	ag := m.ag
	ag.MemoryRecallCh = prefetchCh
	startAgentCmd := func() tea.Msg {
		return agentReadyMsg{ch: ag.Run(ctx, conv)}
	}

	m.updateViewport()
	return m, tea.Batch(tea.Println(userLine), startAgentCmd, m.listenForAskUser(), m.listenForSubAgentProgress(), m.spinner.Tick)
}

func (m Model) sendPromptCommand(displayText, prompt string) (tea.Model, tea.Cmd) {
	wd, _ := os.Getwd()
	_, _ = history.RecordInput(history.InputOptions{Cwd: wd, SessionID: &m.sessionID, Source: "cli", Text: displayText})
	m.historyEntries = append(m.historyEntries, displayText)
	m.historyIndex = 0
	m.historyDraft = ""
	session.SaveMessage(wd, m.sessionID, session.Message{Role: "user", Content: displayText, Ts: time.Now().Unix()})

	m.streaming = true
	m.thinking = true
	m.thinkingStart = time.Now()
	m.thinkingDone = 0
	m.atMenuOpen = false
	m.atMatches = nil
	m.textarea.Reset()
	m.textarea.SetHeight(1)

	m.chatMessages = append(m.chatMessages, chatMessage{role: "user", content: displayText})
	userLine := promptStyle.Render("❯ ") + lipgloss.NewStyle().Foreground(brightText).Bold(true).Render(displayText)
	m.committedUpTo = len(m.chatMessages)
	m.conversation.AddUserMessage(prompt)

	prefetchCh := m.prefetchRelevantMemories(prompt)

	m.streamBuf = ""
	m.toolBlocks = nil
	m.userScrolled = false

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelStream = cancel

	conv := m.conversation
	ag := m.ag
	ag.MemoryRecallCh = prefetchCh
	startAgentCmd := func() tea.Msg {
		return agentReadyMsg{ch: ag.Run(ctx, conv)}
	}

	m.updateViewport()
	return m, tea.Batch(tea.Println(userLine), startAgentCmd, m.listenForAskUser(), m.listenForSubAgentProgress(), m.spinner.Tick)
}
