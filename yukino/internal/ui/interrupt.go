package ui

import (
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

func (m *Model) interruptForeground() {
	if m.cancelStream != nil {
		m.cancelStream()
	}
	if m.permRespCh != nil {
		select {
		case m.permRespCh <- agent.PermissionReply{Response: agent.PermDeny}:
		default:
		}
	}
	m.closePermDialog()
	if m.askUserRespCh != nil {
		select {
		case m.askUserRespCh <- tools.QuestionResponse{Answers: map[string]string{"_declined": "true"}}:
		default:
		}
	}
	m.askUserRespCh = nil
	m.askUserDialog = false
	m.planApprovalDialog = false
	if ch := m.agentCh; ch != nil {
		go func() {
			for range ch {
			}
		}()
	}
	m.savePartialResponse()
	m.finishStreaming()
}
