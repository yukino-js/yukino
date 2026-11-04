package teams

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/plan_file"
	"github.com/yukino-js/yukino/yukino/internal/prompt"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

const LeadName = "leader"
const IdlePollInterval = 500 * time.Millisecond

func CreateIdleNotification(name, reason string) FileMailMessage {
	return NewFileMailMessage(name, fmt.Sprintf("[idle] %s (reason: %s)", name, reason))
}
func RunInProcessTeammate(ctx context.Context, team *Team, member *Member, initialPrompt, addendum string, eventOut chan<- agent.AgentEvent) (runErr error) {
	if addendum != "" {
		member.Conv.AddSystemReminder(addendum)
	}
	defer func() {
		if member.Progress != nil {
			member.Progress.ClearActiveTools()
			if ctx.Err() != nil {
				member.Progress.SetStatus("stopped")
			} else if runErr != nil {
				member.Progress.SetStatus("failed")
			} else {
				member.Progress.SetStatus("completed")
			}
		}
	}()
	nextPrompt := initialPrompt
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		member.Progress.SetStatus("running")
		member.Conv.AddUserMessage(prompt.BuildTeammatePrompt(team.Name, member.Name, nextPrompt))
		var final strings.Builder
		var executionErr error
		for ev := range member.AgentRef.Run(ctx, member.Conv) {
			switch e := ev.(type) {
			case agent.StreamText:
				final.WriteString(e.Text)
			case agent.ToolUseEvent:
				member.Progress.RecordToolStart(e.ToolID, e.ToolName, e.Args)
			case agent.ToolResultEvent:
				member.Progress.RecordToolResult(e.ToolID)
			case agent.TurnComplete:
				member.Progress.RecordTurnComplete()
			case agent.UsageEvent:
				member.Progress.RecordTokens(int64(e.InputTokens), int64(e.OutputTokens))
			case agent.ErrorEvent:
				executionErr = fmt.Errorf("%s", e.Message)
			case agent.PermissionRequestEvent:
				reply := agent.PermissionReply{Response: agent.PermDeny}
				if runtime, ok := tools.RuntimeContext(ctx); ok && runtime.RequestPermission != nil {
					response, feedback := runtime.RequestPermission(ctx, tools.PermissionRequest{ToolName: e.ToolName, Desc: e.Desc, AgentName: e.AgentName, Cwd: e.Cwd, ToolCallID: e.ToolID, Args: e.Args, RememberPatterns: e.Decision.RememberPatterns})
					reply = agent.PermissionReply{Response: agent.PermissionResponse(response), Feedback: feedback}
				}
				e.ResponseCh <- reply
				continue
			}
			if eventOut != nil {
				select {
				case eventOut <- ev:
				case <-ctx.Done():
				}
			}
		}
		member.Progress.ClearActiveTools()
		member.Progress.SetLastMessage(final.String())
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if executionErr != nil {
			return executionErr
		}
		team.mu.Lock()
		planning := planModeActive(member) && (member.PlanApprovalRequired || member.PendingPlan != nil)
		team.mu.Unlock()
		member.Progress.SetStatus("idle")
		if planning {
			next, err := requestPlanApproval(ctx, team, member, final.String())
			if err != nil {
				return err
			}
			if next == "" {
				return nil
			}
			nextPrompt = next
			continue
		}
		if err := team.MailBox.Send(LeadName, CreateIdleNotification(member.Name, "available")); err != nil {
			return err
		}
		next, shutdown, err := waitForNextPromptOrShutdown(ctx, team, member.Name)
		if err != nil {
			return err
		}
		if shutdown != nil {
			_ = team.MailBox.Send(LeadName, NewShutdownResponse(member.Name, shutdown.RequestID, true, ShutdownAckMessage))
			return nil
		}
		nextPrompt = next
	}
}
func planModeActive(member *Member) bool {
	return member.AgentRef != nil && member.AgentRef.Checker != nil && !member.AgentRef.Checker.ReadOnly && member.AgentRef.Checker.Mode() == permissions.ModePlan
}
func requestPlanApproval(ctx context.Context, team *Team, member *Member, fallback string) (string, error) {
	team.mu.Lock()
	pending := member.PendingPlan
	team.mu.Unlock()
	if pending == nil {
		if _, err := team.SubmitPlan(member.Name, readPlanForReview(member, fallback)); err != nil {
			return "", err
		}
	}
	team.mu.Lock()
	requestID := member.PendingPlan.Request.RequestID
	manual := team.RequiresLeaderPlanApproval
	team.mu.Unlock()
	if !manual {
		response := NewPlanApprovalResponse(LeadName, requestID, true, PlanAutoApprovedAckMessage)
		if err := team.MailBox.Send(LeadName, response); err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		exitTeammatePlan(team, member)
		return resumeAfterPlanReview(team, member, PlanAutoApprovedMessage)
	}
	for {
		if err := waitForMember(ctx); err != nil {
			return "", err
		}
		messages, err := team.MailBox.ReceiveMatching(member.Name, func(m FileMailMessage) bool { return IsShutdownRequest(m) || m.Type == MsgPlanApprovalResponse })
		if err != nil {
			return "", err
		}
		for _, m := range messages {
			if IsShutdownRequest(m) {
				_ = team.MailBox.Send(LeadName, NewShutdownResponse(member.Name, m.RequestID, true, StoppedAwaitingPlanReviewMessage))
				return "", nil
			}
		}
		for _, m := range messages {
			if m.From != LeadName || m.RequestID != requestID || m.Approve == nil || strings.TrimSpace(m.Text) == "" {
				continue
			}
			team.mu.Lock()
			member.PendingPlan = nil
			team.mu.Unlock()
			if !m.Approved() {
				return resumeAfterPlanReview(team, member, buildPlanRejectedMessage(m.Text))
			}
			exitTeammatePlan(team, member)
			return resumeAfterPlanReview(team, member, buildPlanApprovedMessage(m.Text))
		}
	}
}
func exitTeammatePlan(team *Team, member *Member) {
	team.mu.Lock()
	mode := team.PermissionMode
	member.PendingPlan = nil
	member.PlanApprovalRequired = false
	team.mu.Unlock()
	if mode == permissions.ModePlan {
		mode = permissions.ModeDefault
	}
	member.AgentRef.Checker.ExitPlanMode(mode)
}
func readPlanForReview(member *Member, fallback string) string {
	content, err := plan_file.LoadPlan(member.AgentRef.Checker)
	if err == nil && strings.TrimSpace(content) != "" {
		return content
	}
	if fallback != "" {
		return fallback
	}
	return "(The teammate returned no plan.)"
}
func resumeAfterPlanReview(team *Team, member *Member, review string) (string, error) {
	messages, err := team.MailBox.ReceiveMatching(member.Name, func(m FileMailMessage) bool { return m.Type == "" || m.Type == MsgText })
	if err != nil {
		return "", err
	}
	if len(messages) == 0 {
		return review, nil
	}
	return review + "\n\nMessages received while awaiting plan review. Apply scope corrections before executing or revising the plan:\n\n" + formatMailboxLines(messages), nil
}
func buildPlanApprovedMessage(feedback string) string {
	return "The leader approved your plan. Review feedback:\n" + feedback + "\n\nBegin execution within the approved scope and current tool permissions; plan approval does not authorize individual tool calls."
}
func buildPlanRejectedMessage(feedback string) string {
	return "The leader rejected your plan. Review feedback:\n" + feedback + "\n\nStay in plan mode. Revise the plan and submit it again with ExitPlanMode when available, otherwise return the revised plan in your final response."
}
func waitForMember(ctx context.Context) error {
	timer := time.NewTimer(IdlePollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func waitForNextPromptOrShutdown(ctx context.Context, team *Team, name string) (string, *FileMailMessage, error) {
	for {
		if err := waitForMember(ctx); err != nil {
			// The member was deactivated while waiting: surface a synthetic
			// shutdown request from the leader so the run loop sends a shutdown
			// acknowledgment, mirroring the reference's idle-deactivation path.
			synthetic := NewShutdownRequest(LeadName, MemberDeactivatedShutdownReason)
			return "", &synthetic, nil
		}
		messages, err := team.MailBox.ReceiveMatching(name, func(FileMailMessage) bool { return true })
		if err != nil {
			return "", nil, err
		}
		if len(messages) == 0 {
			continue
		}
		for i, m := range messages {
			if IsShutdownRequest(m) {
				other := append(append([]FileMailMessage{}, messages[:i]...), messages[i+1:]...)
				if err := team.MailBox.Requeue(name, other); err != nil {
					return "", nil, err
				}
				return "", &m, nil
			}
		}
		return formatInboundAsPrompt(messages), nil, nil
	}
}
func formatMailboxLines(messages []FileMailMessage) string {
	lines := []string{}
	for _, m := range messages {
		lines = append(lines, fmt.Sprintf("From %s: %s", m.From, m.Text))
	}
	return strings.Join(lines, "\n\n")
}
func formatInboundAsPrompt(messages []FileMailMessage) string {
	if len(messages) == 0 {
		return ""
	}
	return "You have new messages from your team:\n\n" + formatMailboxLines(messages)
}

// HasLeaderNotifications reports whether any team's leader mailbox has unread
// messages, without consuming them. It mirrors TeamManager.hasLeaderNotifications.
func HasLeaderNotifications(mgr *TeamManager) bool {
	if mgr == nil {
		return false
	}
	for _, name := range mgr.ListTeams() {
		team := mgr.GetTeam(name)
		if team == nil {
			continue
		}
		messages, err := team.MailBox.ReadUnread(LeadName)
		if err == nil && len(messages) > 0 {
			return true
		}
	}
	return false
}

func DrainLeadMailbox(mgr *TeamManager) []string {
	if mgr == nil {
		return nil
	}
	var notes []string
	for _, name := range mgr.ListTeams() {
		team := mgr.GetTeam(name)
		messages, err := team.MailBox.ReceiveMatching(LeadName, func(FileMailMessage) bool { return true })
		if err == nil && len(messages) > 0 {
			notes = append(notes, buildTeamNotification(name, messages))
		}
	}
	return notes
}
func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(s)
}
func buildTeamNotification(name string, messages []FileMailMessage) string {
	lines := []string{`<task-notification team="` + escapeXML(name) + `">`}
	for _, m := range messages {
		attributes := `from="` + escapeXML(m.From) + `"`
		if m.Type != "" {
			attributes += ` type="` + escapeXML(m.Type) + `"`
		}
		if m.RequestID != "" {
			attributes += ` requestId="` + escapeXML(m.RequestID) + `"`
		}
		if m.Approve != nil {
			attributes += fmt.Sprintf(` approve="%t"`, *m.Approve)
		}
		lines = append(lines, "<message "+attributes+">"+escapeXML(m.Text)+"</message>")
	}
	return strings.Join(append(lines, "</task-notification>"), "\n")
}
