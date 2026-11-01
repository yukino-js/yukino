package remote

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/plan_file"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/prompt"
)

func (s *Server) planApprovalMessage(id string) wsMessage {
	path, content := "", ""
	if s.ag != nil && s.ag.Checker != nil {
		path = s.ag.Checker.CurrentPlanPath()
		content, _ = plan_file.LoadPlan(s.ag.Checker)
	}
	return wsMessage{Type: "plan_approval_request", Data: map[string]string{"id": id, "planPath": path, "planContent": content}}
}
func (s *Server) requestPlanApproval() {
	s.planMu.Lock()
	old := s.planApprovalID
	if s.planApprovalTimer != nil {
		s.planApprovalTimer.Stop()
	}
	id := "plan-" + uuid.NewString()
	s.planApprovalID = id
	s.planApprovalTimer = time.AfterFunc(10*time.Minute, func() {
		s.planMu.Lock()
		if s.planApprovalID != id {
			s.planMu.Unlock()
			return
		}
		s.planApprovalID = ""
		s.planMu.Unlock()
		s.send(wsMessage{Type: "plan_approval_closed", Data: map[string]string{"id": id}})
		s.send(wsMessage{Type: "system", Data: map[string]string{"message": "Plan approval expired after 10 minutes."}})
	})
	s.planMu.Unlock()
	if old != "" {
		s.send(wsMessage{Type: "plan_approval_closed", Data: map[string]string{"id": old}})
	}
	s.send(s.planApprovalMessage(id))
}
func (s *Server) handlePlanApprovalResponse(id, choice, feedback string) {
	if choice != "yolo" && choice != "manual" && choice != "feedback" {
		return
	}
	s.planMu.Lock()
	if id == "" || s.planApprovalID != id || s.ag == nil || s.ag.Checker == nil {
		s.planMu.Unlock()
		return
	}
	s.planApprovalID = ""
	if s.planApprovalTimer != nil {
		s.planApprovalTimer.Stop()
		s.planApprovalTimer = nil
	}
	s.planMu.Unlock()
	s.send(wsMessage{Type: "plan_approval_closed", Data: map[string]string{"id": id}})
	if choice == "feedback" {
		if text := strings.TrimSpace(feedback); text != "" {
			s.queuePlanExecution(text)
		}
		return
	}
	checker := s.ag.Checker
	path := checker.CurrentPlanPath()
	content, _ := plan_file.LoadPlan(checker)
	mode, message := permissions.ModeDefault, "Plan approved. Each edit requires confirmation."
	if choice == "yolo" {
		mode = permissions.ModeBypass
		message = "Plan approved. Entered YOLO mode."
	}
	checker.ExitPlanMode(mode)
	s.conv.AddSystemReminder(prompt.BuildPlanModeExitReminder(path, content != ""))
	s.send(wsMessage{Type: "system", Data: map[string]string{"message": message}})
	if content != "" {
		s.queuePlanExecution("Execute this plan:\n\n" + content)
	}
}
func (s *Server) queuePlanExecution(text string) {
	s.runMu.Lock()
	if s.streaming {
		s.pendingPlanExecution = text
		s.runMu.Unlock()
		return
	}
	s.runMu.Unlock()
	go s.handleUserMessage(text)
}
func (s *Server) beginRun() (context.Context, bool) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.streaming {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.streaming = true
	s.cancelStream = cancel
	return ctx, true
}
func (s *Server) finishRun() {
	s.runMu.Lock()
	s.streaming = false
	s.cancelStream = nil
	s.agentCh = nil
	pending := s.pendingPlanExecution
	s.pendingPlanExecution = ""
	s.runMu.Unlock()
	if pending != "" {
		go s.handleUserMessage(pending)
	}
}
