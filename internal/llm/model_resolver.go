package llm

import "github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"

func NewModelResolver(baseCfg config.ProviderConfig) func(string) (Client, error) {
	return func(model string) (Client, error) {
		cfg := baseCfg
		cfg.Model = model
		return NewClient(&cfg, "")
	}
}
