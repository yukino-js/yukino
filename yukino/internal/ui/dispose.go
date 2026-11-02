package ui

import "errors"

func (m Model) Dispose() error {
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
	var failures []error
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
