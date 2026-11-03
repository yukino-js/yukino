package remote

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/plan_file"
	"github.com/yukino-js/yukino/yukino/internal/prompt"
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
	s.planMu.Lock()
	s.hasExitedPlanMode = true
	s.planMu.Unlock()
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
	go s.handleUserMessage(text, nil)
}
func (s *Server) beginRun() (context.Context, bool) {
	s.runMu.Lock()
	if s.streaming {
		s.runMu.Unlock()
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.streaming = true
	s.cancelStream = cancel
	s.runMu.Unlock()
	s.send(wsMessage{Type: "run_state", Data: map[string]any{"streaming": true}})
	return ctx, true
}
func (s *Server) finishRun() {
	s.runMu.Lock()
	wasStreaming := s.streaming
	s.streaming = false
	s.cancelStream = nil
	s.agentCh = nil
	pending := s.pendingPlanExecution
	s.pendingPlanExecution = ""
	s.runMu.Unlock()
	if wasStreaming {
		s.send(wsMessage{Type: "run_state", Data: map[string]any{"streaming": false}})
	}
	if pending != "" {
		s.handleUserMessage(pending, nil)
	}
	s.replaySteeringLeftovers()
	s.drainFollowUps()
}
