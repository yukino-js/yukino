package ui

import (
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/commands"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

var goalControl = regexp.MustCompile(`^/goal(?:\s+(status|pause|clear|complete))?$`)

type foregroundSettledMsg struct{ ch <-chan agent.AgentEvent }

type ctrlCExpiredMsg struct{ generation int }

func (m Model) modalActive() bool {
	return m.loginDialog || m.providerDialog || m.modelDialog || m.planApprovalDialog || m.rewindDialog || m.resumeDialog || m.permDialog || m.askUserDialog || m.sandboxDialog
}

func (m Model) foregroundBusy() bool {
	return m.streaming || m.compacting || m.startingAgent || m.settlingCh != nil || m.activeSubAgent != nil && !m.activeSubAgent.done
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.String() == "ctrl+c" {
			if m.foregroundBusy() {
				m.interruptForeground()
				m.ctrlCDeadline = time.Time{}
				m.ctrlCGeneration++
				cmd := m.waitForForegroundCleanup()
				return m, cmd
			}
			if !m.ctrlCDeadline.IsZero() && time.Now().Before(m.ctrlCDeadline) {
				return m, tea.Quit
			}
			if !m.modalActive() {
				m.clearDraft()
			}
			m.ctrlCDeadline = time.Now().Add(2 * time.Second)
			m.ctrlCGeneration++
			generation := m.ctrlCGeneration
			return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return ctrlCExpiredMsg{generation} })
		}
		if !m.ctrlCDeadline.IsZero() {
			m.ctrlCDeadline = time.Time{}
			m.ctrlCGeneration++
		}
		if key.String() == "ctrl+o" {
			m.toolsExpanded = !m.toolsExpanded
			for i := range m.chatMessages {
				m.chatMessages[i].expanded = m.toolsExpanded
			}
			m.updateViewport()
			text := m.renderTranscriptHeader() + "\n" + m.renderMessagesRange(0, m.committedUpTo)
			return m, tea.Sequence(tea.ClearScreen, tea.Println(text))
		}
	}
	switch event := msg.(type) {
	case foregroundSettledMsg:
		if event.ch == m.settlingCh {
			m.settlingCh = nil
			m.settleWaiting = false
		}
	case agentReadyMsg:
		m.startingAgent = false
		if !m.streaming {
			m.settlingCh = event.ch
			cmd := m.waitForForegroundCleanup()
			return m, cmd
		}
	case ctrlCExpiredMsg:
		if event.generation == m.ctrlCGeneration {
			m.ctrlCDeadline = time.Time{}
		}
		return m, nil
	case clipboardImageMsg:
		m.handleClipboard(event)
		return m, nil
	}
	updated, cmd := m.update(msg)
	var next Model
	switch value := updated.(type) {
	case Model:
		next = value
	case *Model:
		next = *value
	}
	if event, ok := msg.(agentEventMsg); ok && m.agentCh == event.ch {
		switch event.event.(type) {
		case agent.LoopComplete, agent.ErrorEvent:
			next.settlingCh = event.ch
		}
	}
	cleanupCmd := next.waitForForegroundCleanup()
	cmd = tea.Batch(cmd, cleanupCmd)

	if next.foregroundBusy() {
		next.ctrlCDeadline = time.Time{}
		next.ctrlCGeneration++
	}
	if !next.foregroundBusy() && !next.modalActive() && next.state == stateChat && next.ag != nil && len(next.pendingMessages) > 0 && !next.queuePaused {
		text := next.pendingMessages[0]
		next.pendingMessages = next.pendingMessages[1:]
		dispatched, dispatchCmd := next.dispatchInput(text)
		switch value := dispatched.(type) {
		case Model:
			next = value
		case *Model:
			next = *value
		}
		if next.activityError != "" {
			next.pendingMessages = append([]string{text}, next.pendingMessages...)
			next.queuePaused = true
		}
		return next, tea.Batch(cmd, dispatchCmd)
	}
	return next, cmd
}

func (m Model) backgroundShells() tea.Cmd {
	if !m.modalActive() && tools.HasAnyForegroundTasks(m.registry) {
		tools.BackgroundAllForegroundTasks(m.registry)
	}
	return nil
}

func (m Model) submitInput(text string) (tea.Model, tea.Cmd) {
	if m.foregroundBusy() && goalControl.FindStringSubmatch(text) == nil {
		m.pendingMessages = append(m.pendingMessages, text)
		m.queuePaused = false
		m.clearDraft()
		m.updateViewport()
		return m, nil
	}
	var updated tea.Model
	var cmd tea.Cmd
	if match := goalControl.FindStringSubmatch(text); m.foregroundBusy() && match != nil {
		updated, cmd = m.runGoalCommand(match[1])
	} else {
		updated, cmd = m.dispatchInput(text)
	}
	var next Model
	switch value := updated.(type) {
	case Model:
		next = value
	case *Model:
		next = *value
	}
	if next.activityError == "" {
		next.clearDraft()
	}
	return next, cmd
}

func (m Model) dispatchInput(text string) (tea.Model, tea.Cmd) {
	m.activityError = ""
	if strings.HasPrefix(text, "/") {
		name, args, ok := commands.Parse(text)
		if ok {
			return m.executeCommand(name, args)
		}
	}
	if m.ag == nil {
		m.activityError = "LLM client not ready yet"
		return m, nil
	}
	return m.sendMessage(text)
}

func (m *Model) recallQueued() (string, bool) {
	if len(m.pendingMessages) == 0 {
		return "", false
	}
	last := len(m.pendingMessages) - 1
	text := m.pendingMessages[last]
	m.pendingMessages = m.pendingMessages[:last]
	return text, true
}

func (m *Model) waitForForegroundCleanup() tea.Cmd {
	if m.settlingCh == nil || m.settleWaiting {
		return nil
	}
	m.settleWaiting = true
	ch := m.settlingCh
	return func() tea.Msg {
		for range ch {
		}
		return foregroundSettledMsg{ch: ch}
	}
}
