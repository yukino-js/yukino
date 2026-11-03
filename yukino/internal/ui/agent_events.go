package ui

import (
	"fmt"
	"math"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
	"github.com/yukino-js/yukino/yukino/internal/tool_result"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

type agentEventMsg struct {
	event agent.AgentEvent
	ch    <-chan agent.AgentEvent
}

type agentDoneMsg struct {
	ch <-chan agent.AgentEvent
}

type agentErrMsg struct{ err error }

type agentReadyMsg struct {
	ch <-chan agent.AgentEvent
}

type mcpReadyMsg struct {
	result mcp.ConnectResult
	mgr    *mcp.Manager
}

type compactDoneMsg struct {
	message string
	err     error
}

type forkSkillDoneMsg struct {
	name   string
	result string
	err    error
}

type askUserMsg struct {
	req tools.AskUserRequest
}

type subAgentProgressMsg struct {
	progress subagent.SubAgentProgress
}

func (m *Model) drainSubAgentProgress() {
	for {
		select {
		case p := <-m.subAgentProgressCh:
			m.applySubagentProgress(p)
		default:
			return
		}
	}
}

type outputFlushMsg struct{ generation int }

func (m *Model) scheduleOutputFlush() tea.Cmd {
	if m.flushScheduled {
		return nil
	}
	m.flushScheduled = true
	generation := m.outputGeneration
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return outputFlushMsg{generation} })
}

func (m *Model) prepareOutput() {
	m.outputGeneration++
	m.flushScheduled = false
	m.streamBuf = ""
	m.thinkingBuf = ""
	m.thinkingTextStart = time.Time{}
	m.thinkingDuration = 0
	m.toolBlocks = nil
	for id, progress := range m.subagents {
		if progress.taskID == "" || progress.status != "running" {
			delete(m.subagents, id)
		}
	}
}

func (m *Model) commitOutput() {
	m.outputGeneration++
	m.flushScheduled = false
	if m.thinkingBuf != "" || m.thinkingDuration >= 1 {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "turn_summary", content: m.thinkingBuf, thinkingDuration: m.thinkingDuration})
	}
	if m.streamBuf != "" {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "assistant", content: m.streamBuf})
	}
	m.streamBuf = ""
	m.thinkingBuf = ""
	m.thinkingTextStart = time.Time{}
	m.thinkingDuration = 0
	var completed, pending []toolBlockInfo
	for _, tb := range m.toolBlocks {
		if tb.loading {
			pending = append(pending, tb)
		} else {
			completed = append(completed, tb)
		}
	}
	if len(completed) > 0 {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "turn_summary", toolGroup: completed})
	}
	m.toolBlocks = pending
}

func (m *Model) commitStaticOutput() tea.Cmd {
	if m.committedUpTo >= len(m.chatMessages) {
		return nil
	}
	text := m.renderMessagesRange(m.committedUpTo, len(m.chatMessages))
	m.committedUpTo = len(m.chatMessages)
	if text == "" {
		return nil
	}
	return tea.Println(text)
}

func (m Model) listenForSubAgentProgress() tea.Cmd {
	ch := m.subAgentProgressCh
	if ch == nil {
		return nil
	}
	life := m.lifeCtx
	return func() tea.Msg {
		select {
		case p := <-ch:
			return subAgentProgressMsg{progress: p}
		case <-life.Done():
			return nil
		}
	}
}

func (m Model) listenForAskUser() tea.Cmd {
	ch := m.askUserCh
	if ch == nil {
		return nil
	}
	life := m.lifeCtx
	return func() tea.Msg {
		select {
		case req := <-ch:
			return askUserMsg{req: req}
		case <-life.Done():
			return nil
		}
	}
}

func (m Model) listenForAgentEvents() tea.Cmd {
	ch := m.agentCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return agentDoneMsg{ch: ch}
		}
		return agentEventMsg{event: ev, ch: ch}
	}
}

func (m Model) handleAgentEvent(ev agent.AgentEvent) (tea.Model, tea.Cmd) {
	m.recordInteractionEvent(ev)
	switch ev.(type) {
	case agent.RetryEvent, agent.UsageEvent, agent.PermissionRequestEvent:
	default:
		m.retryStatus = ""
	}
	switch e := ev.(type) {
	case agent.StreamText:
		m.streamBuf += e.Text
		return m, tea.Batch(m.scheduleOutputFlush(), m.listenForAgentEvents())

	case agent.ThinkingText:
		if m.thinkingTextStart.IsZero() {
			m.thinkingTextStart = time.Now()
		}
		m.thinkingBuf += e.Text
		return m, tea.Batch(m.scheduleOutputFlush(), m.listenForAgentEvents())

	case agent.ThinkingComplete:
		if !m.thinkingTextStart.IsZero() {
			m.thinkingDuration = time.Since(m.thinkingTextStart).Seconds()
		}

	case agent.ToolUseEvent:
		m.toolBlocks = append(m.toolBlocks, toolBlockInfo{toolID: e.ToolID, toolName: e.ToolName, args: e.Args, loading: true})

	case agent.ToolResultEvent:
		if e.ToolName == "WriteFile" || e.ToolName == "EditFile" {
			m.fileFactsVersion++
		}
		m.drainSubAgentProgress()
		for i := range m.toolBlocks {
			if m.toolBlocks[i].toolID == e.ToolID {
				tb := &m.toolBlocks[i]
				tb.output = tool_result.ToDisplayPreview(e.Output)
				tb.isError = e.IsError
				tb.elapsed = e.Elapsed.Seconds()
				tb.loading = false
				if p := m.subagents[e.ToolID]; p != nil && p.status != "running" {
					tb.status = p.status
					if e.IsError && tb.status != "stopped" {
						tb.status = "failed"
					}
					tb.progress = fmt.Sprintf("%s subagent | %d turns", p.role, p.turns)
				}
				break
			}
		}
		if e.ToolName == "AskUserQuestion" {
			m.commitOutput()
			cmd := m.commitStaticOutput()
			return m, tea.Batch(cmd, m.listenForAgentEvents())
		}

	case agent.TurnComplete:
		m.commitOutput()
		m.toolBlocks = nil
		cmd := m.commitStaticOutput()
		return m, tea.Batch(cmd, m.listenForAgentEvents())

	case agent.UsageEvent:
		m.totalInput += e.InputTokens
		m.totalOutput += e.OutputTokens
		m.totalCacheRead += e.CacheReadTokens
		m.totalCacheCreation += e.CacheCreationTokens
	case agent.SteeringDelivered:
		m.deliverSteering(e)
		return m, tea.Batch(m.commitStaticOutput(), m.listenForAgentEvents())

	case agent.PermissionRequestEvent:
		m.enqueuePermission(e)

	case agent.CompactEvent:
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "system",
			content: "⊙ " + e.Message,
		})

	case agent.RetryEvent:
		m.retryStatus = "Retrying"
		if e.Wait > 0 {
			// Math.round (half-up) in the reference, not Go's half-to-even %.0f.
			m.retryStatus += fmt.Sprintf(" (%ds delay)", int(math.Round(e.Wait.Seconds())))
		}
		m.retryStatus += ": " + e.Reason
		msg := "↻ " + e.Reason
		if e.Wait > 0 {
			msg += fmt.Sprintf(" (waiting %ds)", int(math.Round(e.Wait.Seconds())))
		}
		m.chatMessages = append(m.chatMessages, chatMessage{
			role:    "system",
			content: msg,
		})

	case agent.ErrorEvent:
		m.activityError = e.Message
		m.commitOutput()
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "Error: " + e.Message})
		cmd := m.commitStaticOutput()
		m.toolBlocks = nil
		m.finishStreaming()
		return m, cmd

	case agent.LoopComplete:
		m.commitOutput()
		m.toolBlocks = nil
		commitCmd := m.commitStaticOutput()
		m.finishStreaming()
		if e.PlanReady && m.ag != nil && m.ag.Checker != nil && m.ag.Checker.Mode() == permissions.ModePlan {
			m.planApprovalDialog = true
			m.planApprovalCursor = 0
			m.planApprovalInput = ""
			m.planFeedbackRequired = false
		}
		return m, commitCmd
	}

	return m, m.listenForAgentEvents()
}
