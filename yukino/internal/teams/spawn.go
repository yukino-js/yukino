package teams

import (
	"context"
	"fmt"

	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

type TeammateSpawnConfig struct {
	ContextSnapshot      *conversation.Manager
	MaxTurns             int
	Team                 *Team
	MemberName           string
	Task                 string
	Addendum             string
	Client               llm.Client
	Registry             *tools.Registry
	Protocol             string
	Workdir              string
	Checker              *permissions.Checker
	PlanApprovalRequired bool
	AgentType            string
	Model                string
	OriginToolCallID     string
}
type SpawnResult struct{ EventCh <-chan agent.AgentEvent }

func SpawnTeammate(ctx context.Context, cfg TeammateSpawnConfig) (*SpawnResult, error) {
	if cfg.Team == nil {
		return nil, fmt.Errorf("SpawnTeammate: team is required")
	}
	ch, err := startInProcessMember(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &SpawnResult{EventCh: ch}, nil
}
