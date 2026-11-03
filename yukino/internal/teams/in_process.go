package teams

import (
	"context"
	"fmt"

	"github.com/yukino-js/yukino/yukino/internal/memory"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/plan_file"

	"github.com/yukino-js/yukino/yukino/internal/agent"
)

func startInProcessMember(ctx context.Context, cfg TeammateSpawnConfig) (<-chan agent.AgentEvent, error) {
	team, memberName := cfg.Team, cfg.MemberName
	if cfg.Checker != nil {
		cfg.Checker.RequiresLeaderPlanApproval = team.RequiresLeaderPlanApproval
		if cfg.Checker.Mode() == permissions.ModePlan {
			if _, err := plan_file.GetOrCreatePlanPath(cfg.Checker); err != nil {
				return nil, fmt.Errorf("create teammate plan: %w", err)
			}
		}
	}
	member, err := team.AddMember(memberName, cfg.Client, cfg.Registry, cfg.Protocol)
	if err != nil {
		return nil, err
	}
	team.mu.Lock()
	member.AgentRef.Checker = cfg.Checker
	member.AgentRef.MaxIterations = cfg.MaxTurns
	member.AgentRef.DisableBackground = true
	member.AgentRef.Instructions = memory.LoadInstructions(cfg.Workdir)
	member.PlanApprovalRequired = cfg.PlanApprovalRequired

	if cfg.Workdir != "" {
		member.AgentRef.WorkDir = cfg.Workdir
	}
	if cfg.ContextSnapshot != nil {
		member.Conv = cfg.ContextSnapshot
	}

	member.Progress = NewTeammateProgress(memberName, team.Name, TeammateSpinnerVerb)

	memberCtx, cancel := context.WithCancel(ctx)
	member.Active = true
	member.Cancel = cancel
	member.Done = make(chan struct{})
	done := member.Done
	GetNameRegistry().Register(memberName, memberName)
	team.persist()
	team.mu.Unlock()

	eventCh := make(chan agent.AgentEvent, 32)
	go func() {
		defer close(eventCh)
		defer close(done)
		defer func() {
			_ = cfg.Registry.Dispose()
			team.mu.Lock()
			member.Active = false
			member.PendingPlan = nil
			member.Cancel = nil
			GetNameRegistry().Unregister(memberName)
			team.persist()
			team.pruneTerminatedMembers()
			team.mu.Unlock()
			team.releaseMemberTasks(memberName)
		}()
		_ = RunInProcessTeammate(memberCtx, team, member, cfg.Task, cfg.Addendum, eventCh)
	}()
	return eventCh, nil
}
