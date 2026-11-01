package teams

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/plan_file"
)

const LeadName = "lead"

const ShutdownPrefix = "[shutdown]"

const IdlePollInterval = 500 * time.Millisecond

func CreateIdleNotification(memberName, reason string) FileMailMessage {
	return NewFileMailMessage(memberName, fmt.Sprintf("[idle] %s (reason: %s)", memberName, reason))
}

func RunInProcessTeammate(
	ctx context.Context,
	team *Team,
	member *Member,
	initialPrompt string,
	addendum string,
	eventOut chan<- agent.AgentEvent,
) error {
	if addendum != "" {
		member.Conv.AddSystemReminder(addendum)
	}

	nextPrompt := initialPrompt
	idleReason := "available"

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if reminder := InjectPendingMessages(team, member.Name); reminder != "" {
			member.Conv.AddSystemReminder(reminder)
		}

		if nextPrompt != "" {
			member.Conv.AddUserMessage(nextPrompt)
		}
		nextPrompt = ""

		ch := member.AgentRef.Run(ctx, member.Conv)
		for ev := range ch {
			if member.Progress != nil {
				switch e := ev.(type) {
				case agent.ToolUseEvent:
					member.Progress.RecordToolUse(e.ToolName, e.Args)
				case agent.UsageEvent:
					member.Progress.RecordTokens(int64(e.InputTokens), int64(e.OutputTokens))
				}
			}
			if eventOut != nil {
				select {
				case eventOut <- ev:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if e, ok := ev.(agent.ErrorEvent); ok && e.Message != "" {
				idleReason = "failed"
			}
		}

		if member.Progress != nil {
			if idleReason == "failed" {
				member.Progress.SetStatus("failed")
			} else {
				member.Progress.SetStatus("idle")
			}
		}

		if planModeActive(member) {
			approved, feedback, err := requestPlanApproval(ctx, team, member)
			if err != nil {
				return err
			}
			if approved {
				member.AgentRef.Checker.SetMode(permissions.ModeDefault)
				nextPrompt = buildPlanApprovedMessage("")
			} else {
				nextPrompt = buildPlanRejectedMessage(feedback)
			}
			continue
		}

		_ = team.MailBox.Send(LeadName, CreateIdleNotification(member.Name, idleReason))
		idleReason = "available"

		prompt, shutdown, err := waitForNextPromptOrShutdown(ctx, team, member.Name)
		if err != nil {
			return err
		}
		if shutdown != nil {
			if shutdown.Type == MsgShutdownRequest {
				_ = team.MailBox.Send(LeadName,
					NewShutdownResponse(member.Name, shutdown.RequestID, true, "acknowledged, shutting down"))
			}
			return nil
		}
		nextPrompt = prompt
	}
}

func planModeActive(member *Member) bool {
	return member.AgentRef != nil &&
		member.AgentRef.Checker != nil &&
		member.AgentRef.Checker.Mode() == permissions.ModePlan
}

func requestPlanApproval(ctx context.Context, team *Team, member *Member) (bool, string, error) {
	plan := readPlanForReview(member)
	req := NewPlanApprovalRequest(member.Name, plan)
	if err := team.MailBox.Send(LeadName, req); err != nil {
		return false, "", err
	}
	if member.Progress != nil {
		member.Progress.SetStatus("awaiting plan approval")
	}

	for {
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		case <-time.After(IdlePollInterval):
		}

		msgs, err := team.MailBox.ReadUnread(member.Name)
		if err != nil {
			return false, "", err
		}
		for _, m := range msgs {
			if m.Type == MsgPlanApprovalResponse && m.RequestID == req.RequestID {
				_ = team.MailBox.MarkAllRead(member.Name)
				return m.Approved(), m.Text, nil
			}
		}
	}
}

func readPlanForReview(member *Member) string {
	plan_file.GetOrCreatePlanPath()
	content, err := plan_file.LoadPlan()
	if err != nil || content == "" {
		return "(the plan file is empty; the teammate may not have written the plan as required)"
	}
	return content
}

func buildPlanApprovedMessage(feedback string) string {
	return "The leader approved your plan. Review feedback:\n" + feedback +
		"\n\nBegin execution within the approved scope and current tool permissions; plan approval does not authorize individual tool calls."
}

func buildPlanRejectedMessage(feedback string) string {
	return "The leader rejected your plan. Review feedback:\n" + feedback +
		"\n\nStay in plan mode. Revise the plan and submit it again with ExitPlanMode when available, otherwise return the revised plan in your final response."
}

func waitForNextPromptOrShutdown(ctx context.Context, team *Team, memberName string) (string, *FileMailMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(IdlePollInterval):
		}

		msgs, err := team.MailBox.ReadUnread(memberName)
		if err != nil {
			return "", nil, err
		}
		if len(msgs) == 0 {
			continue
		}

		var shutdown *FileMailMessage
		var keep []FileMailMessage
		for i, m := range msgs {
			if IsShutdownRequest(m) {
				shutdown = &msgs[i]
				continue
			}
			keep = append(keep, m)
		}
		_ = team.MailBox.MarkAllRead(memberName)

		if shutdown != nil {
			return "", shutdown, nil
		}
		return formatInboundAsPrompt(keep), nil, nil
	}
}

func DrainLeadMailbox(mgr *TeamManager) []string {
	if mgr == nil {
		return nil
	}
	var notes []string
	for _, name := range mgr.ListTeams() {
		team := mgr.GetTeam(name)
		if team == nil {
			continue
		}
		msgs, err := team.MailBox.ReadUnread(LeadName)
		if err != nil || len(msgs) == 0 {
			continue
		}
		var sb strings.Builder
		sb.WriteString("<team-notification team=\"")
		sb.WriteString(name)
		sb.WriteString("\">\n")
		for _, m := range msgs {
			sb.WriteString("from=")
			sb.WriteString(m.From)
			sb.WriteString(": ")
			sb.WriteString(m.Text)
			sb.WriteString("\n")
		}
		sb.WriteString("</team-notification>")
		notes = append(notes, sb.String())
		_ = team.MailBox.MarkAllRead(LeadName)
	}
	return notes
}

func formatInboundAsPrompt(msgs []FileMailMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("You have new messages from your team:\n\n")
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("From %s: %s\n\n", m.From, m.Text))
	}
	return sb.String()
}

var _ = conversation.NewManager
