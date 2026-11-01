package llm

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
)

type Client interface {
	Stream(ctx context.Context, conv *conversation.Manager, tools []map[string]any) (<-chan StreamEvent, <-chan error)
	SetSystemPrompt(prompt string)
}

type MaxTokensSetter interface {
	SetMaxOutputTokens(tokens int)
}

// ThinkingLevelControl is the runtime control of the effective logical
// thinking level.
type ThinkingLevelControl interface {
	// SetThinkingLevel applies the level and returns the effective one, which
	// may be clamped by the provider.
	SetThinkingLevel(level string) string
	GetThinkingLevel() string
	GetSupportedThinkingLevels() []string
}

type OutputBudget struct {
	MaxOutputTokens int
	// Planning allowance for the provider-native reasoning effort.
	ThinkingTokens int
}

type OutputBudgetProvider interface {
	GetOutputBudget() OutputBudget
}

func NewClient(cfg *config.ProviderConfig, systemPrompt string) (Client, error) {
	switch cfg.Protocol {
	case "anthropic":
		return newAnthropicClient(cfg, systemPrompt)
	case "openai":
		return newOpenAIClient(cfg, systemPrompt)
	case "openai-compat":
		return newOpenAICompatClient(cfg, systemPrompt)
	default:
		return nil, fmt.Errorf("unknown protocol: %s", cfg.Protocol)
	}
}
