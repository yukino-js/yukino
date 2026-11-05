package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/memory/background"
)

type memoryLoopMsg struct {
	conv       *conversation.Manager
	generation int
}
type memorySystemMsg string

// wireMemoryTasks mirrors the reference onLoopComplete wiring: the loop
// completion hops onto the UI goroutine, where the background memory tasks
// are created lazily with the current client.
func (m *Model) wireMemoryTasks(ag *agent.Agent) {
	ch, life := m.memoryLoopCh, m.lifeCtx
	generation := m.memoryGeneration
	ag.OnLoopComplete = func(loopConv *conversation.Manager) {
		select {
		case ch <- memoryLoopMsg{conv: loopConv, generation: generation}:
		case <-life.Done():
		}
	}
}

// stopMemoryTasks disposes the lazily created background memory tasks; the
// next loop completion recreates them with the then-current client.
func (m *Model) stopMemoryTasks() {
	m.memoryGeneration++
	if m.memoryTasks != nil {
		m.memoryTasks.Dispose()
		m.memoryTasks = nil
	}
}

func (m Model) listenMemoryLoop() tea.Cmd {
	ch, ctx := m.memoryLoopCh, m.lifeCtx
	return func() tea.Msg {
		select {
		case msg := <-ch:
			return msg
		case <-ctx.Done():
			return nil
		}
	}
}

func (m Model) listenMemorySystem() tea.Cmd {
	ch, ctx := m.memorySystemCh, m.lifeCtx
	return func() tea.Msg {
		select {
		case message := <-ch:
			return memorySystemMsg(message)
		case <-ctx.Done():
			return nil
		}
	}
}

func (m Model) handleMemoryLoop(msg memoryLoopMsg) (tea.Model, tea.Cmd) {
	if msg.generation == m.memoryGeneration && m.lifeCtx.Err() == nil && m.client != nil && m.memoryEnabled {
		if m.memoryTasks == nil {
			ch, life := m.memorySystemCh, m.lifeCtx
			m.memoryTasks = background.New(m.client, m.getWorkDir(), background.Callbacks{
				OnSaved: func(saved []string) {
					select {
					case ch <- "Memory saved: " + strings.Join(saved, ", "):
					case <-life.Done():
					}
				},
				AppendSystem: func(message string) {
					select {
					case ch <- message:
					case <-life.Done():
					}
				},
			})
		}
		m.memoryTasks.Run(msg.conv)
	}
	return m, m.listenMemoryLoop()
}

func (m Model) handleMemorySystem(msg memorySystemMsg) (tea.Model, tea.Cmd) {
	m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: string(msg)})
	return m, m.listenMemorySystem()
}
