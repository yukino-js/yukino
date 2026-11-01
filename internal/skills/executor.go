package skills

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

type SkillHost interface {
	ActivateSkill(name, body string)
	ToolRegistry() *tools.Registry
}

type SkillForkHost interface {
	SkillHost
	RunSubAgent(ctx context.Context, body string, seed []conversation.Message, model string) (string, error)
	SnapshotParentMessages() []conversation.Message
}

func RunInline(_ context.Context, skill *Skill, args string, host SkillHost) (string, error) {
	body := skill.Render(args)
	host.ActivateSkill(skill.Meta.Name, body)
	return body, nil
}

func RunFork(ctx context.Context, skill *Skill, args string, host SkillForkHost) (string, error) {
	body := skill.Render(args)
	seed := buildForkSeed(skill.Meta.ForkContext, host.SnapshotParentMessages())
	return host.RunSubAgent(ctx, body, seed, skill.Meta.Model)
}

func buildForkSeed(mode string, parent []conversation.Message) []conversation.Message {
	switch mode {
	case "full":
		out := make([]conversation.Message, len(parent))
		copy(out, parent)
		return out
	case "recent":
		if len(parent) <= 5 {
			out := make([]conversation.Message, len(parent))
			copy(out, parent)
			return out
		}
		out := make([]conversation.Message, 5)
		copy(out, parent[len(parent)-5:])
		return out
	default:
		return nil
	}
}
