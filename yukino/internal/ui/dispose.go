package ui

import (
	"errors"
	"github.com/yukino-js/yukino/yukino/internal/session"
)

func (m Model) Dispose() error {
	var failures []error
	if m.lifeCancel != nil {
		m.lifeCancel()
	}
	m.denyAllInteractions()
	if run := m.userBash; run != nil {
		run.cancel()
		<-run.done
		run.mu.Lock()
		if !run.handled && run.result != nil {
			if err := session.SaveMessage(run.cwd, run.sessionID, userBashRecord(run, *run.result)); err != nil {
				failures = append(failures, err)
			}
			run.handled = true
		}
		run.mu.Unlock()
	}
	for _, record := range m.pendingBashRecords {
		if err := session.SaveMessage(m.getWorkDir(), m.sessionID, record); err != nil {
			failures = append(failures, err)
		}
	}
	if m.cancelStream != nil {
		m.cancelStream()
	}
	if m.ideCancel != nil {
		m.ideCancel()
	}
	if m.ideConn != nil {
		_ = m.ideConn.Close()
	}
	if m.mcpMgr != nil {
		m.mcpMgr.DisconnectAll()
	}
	if m.teamMgr != nil {
		m.teamMgr.CloseAll()
	}
	if m.memoryTasks != nil {
		m.memoryTasks.Dispose()
	}
	if m.registry != nil {
		if err := m.registry.Dispose(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
