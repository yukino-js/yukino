package ui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
)

type skillForkHost struct {
	Model
	meta skills.SkillMeta
}

func (h skillForkHost) RunSubagent(ctx context.Context, prompt string) (string, error) {
	definition := &subagent.AgentDefinition{AgentType: h.meta.Name, WhenToUse: h.meta.Description, Model: h.meta.Model}
	return subagent.SpawnSubagent(ctx, definition, prompt, h.client, h.registry, h.selectedProvider, h.getWorkDir(), nil, nil, "", h.ag.Checker, subagent.SubagentRunOptions{})
}

func (m Model) startSkillFork(name, args string) (tea.Model, tea.Cmd) {
	var skill *skills.Skill
	if m.skillCatalog != nil {
		skill = m.skillCatalog.Get(name)
	}
	if skill == nil {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "Skill not found: " + name})
		return m, nil
	}
	if m.client == nil {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: ClientNotReady})
		return m, nil
	}
	ctx, cancel := context.WithCancel(m.workflowContext())
	m.cancelStream = cancel
	m.streaming = true
	m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: fmt.Sprintf("Running skill %q in fork mode…", name)})
	host := skillForkHost{Model: m, meta: skill.Meta}
	return m, tea.Batch(m.commitStaticOutput(), m.spinner.Tick, func() tea.Msg {
		result, err := skills.RunFork(ctx, skill, args, host)
		return forkSkillDoneMsg{name: name, result: result, err: err}
	})
}
