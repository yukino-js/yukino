package ui

import "errors"

func (m Model) Dispose() error {
	if m.cancelStream != nil {
		m.cancelStream()
	}
	if m.mcpMgr != nil {
		m.mcpMgr.Shutdown()
	}
	if m.teamMgr != nil {
		m.teamMgr.CloseAll()
	}
	var failures []error
	if m.memoryExtractor != nil {
		if err := m.memoryExtractor.Drain(5000); err != nil {
			failures = append(failures, err)
		}
	}
	if m.registry != nil {
		if err := m.registry.Dispose(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
