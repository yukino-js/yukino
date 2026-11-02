package ui

func (m *Model) interruptForeground() {
	if m.cancelStream != nil {
		m.cancelStream()
	}
	m.denyAllInteractions()
	m.planApprovalDialog = false
	if ch := m.agentCh; ch != nil {
		m.settlingCh = ch
	}
	m.savePartialResponse()
	m.finishStreaming()
}
