package ui

import (
	"context"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/commands"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/history"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
)

func (m Model) sendMessage(text string) (tea.Model, tea.Cmd) {
	m.refreshSkillsIfNeeded()
	wd, _ := os.Getwd()
	input, err := history.RecordInput(history.InputOptions{Cwd: wd, SessionID: &m.sessionID, Source: "cli", Text: text})
	if err != nil {
		return m.inputError(err)
	}
	expanded := conversation.ExpandAtRefs(text, wd)
	recorded, err := session.SaveUserMessage(wd, m.sessionID, input, session.Message{Content: expanded})
	if err != nil {
		return m.inputError(err)
	}
	m.historyEntries = append(m.historyEntries, text)
	m.historyIndex = 0
	m.historyDraft = ""

	m.streaming = true
	m.thinking = true
	m.thinkingStart = time.Now()
	m.thinkingDone = 0
	m.atMenuOpen = false
	m.atMatches = nil
	m.textarea.Reset()
	m.textarea.SetHeight(1)

	m.chatMessages = append(m.chatMessages, chatMessage{role: "user", content: text})
	userLine := promptStyle.Render("❯ ") + lipgloss.NewStyle().Foreground(brightText).Bold(true).Render(text)
	m.committedUpTo = len(m.chatMessages)
	m.conversation.AppendMessages([]conversation.Message{recorded.ToConversation()})

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

func (m Model) sendPromptCommand(displayText, prompt, agentRole string) (tea.Model, tea.Cmd) {
	wd, _ := os.Getwd()
	name, _, _ := commands.Parse(displayText)
	input, err := history.RecordInput(history.InputOptions{Cwd: wd, SessionID: &m.sessionID, Source: "cli", Text: displayText, CommandName: &name})
	if err != nil {
		return m.inputError(err)
	}
	m.historyEntries = append(m.historyEntries, displayText)
	m.historyIndex = 0
	m.historyDraft = ""

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
	if agentRole == "" {
		recorded, err := session.SaveUserMessage(wd, m.sessionID, input, session.Message{Content: prompt})
		if err != nil {
			return m.inputError(err)
		}
		m.conversation.AppendMessages([]conversation.Message{recorded.ToConversation()})
	}

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
		if agentRole != "" {
			return agentReadyMsg{ch: commands.RunSubagentCommand(ctx, agentRole, prompt, displayText, m.registry, ag.Checker, conv, wd, m.sessionID, &input)}
		}
		return agentReadyMsg{ch: ag.Run(ctx, conv)}
	}

	m.updateViewport()
	return m, tea.Batch(tea.Println(userLine), startAgentCmd, m.listenForAskUser(), m.listenForSubAgentProgress(), m.spinner.Tick)
}

func (m Model) inputError(err error) (tea.Model, tea.Cmd) {
	m.chatMessages = append(m.chatMessages, chatMessage{role: "error", content: err.Error()})
	m.updateViewport()
	return m, nil
}
