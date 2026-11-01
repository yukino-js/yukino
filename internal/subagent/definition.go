package subagent

import (
	"fmt"
	"os"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/frontmatter"

	"github.com/goccy/go-yaml"
)

type AgentMemoryScope string

const (
	AgentMemoryScopeUser    AgentMemoryScope = "user"
	AgentMemoryScopeProject AgentMemoryScope = "project"
	AgentMemoryScopeLocal   AgentMemoryScope = "local"
)

type IsolationMode string

const (
	IsolationWorktree IsolationMode = "worktree"
	IsolationRemote   IsolationMode = "remote"
)

type AgentDefinition struct {
	AgentType       string   `yaml:"name"`
	WhenToUse       string   `yaml:"description"`
	Tools           []string `yaml:"tools"`
	DisallowedTools []string `yaml:"disallowedTools"`
	Model           string   `yaml:"model"`
	MaxTurns        int      `yaml:"maxTurns"`

	PermissionMode string `yaml:"permissionMode"`

	Effort any `yaml:"effort"`

	Skills []string `yaml:"skills"`

	McpServers []any `yaml:"mcpServers"`

	RequiredMcpServers []string `yaml:"requiredMcpServers"`

	Hooks any `yaml:"hooks"`

	Memory AgentMemoryScope `yaml:"memory"`

	Background bool `yaml:"background"`

	Isolation IsolationMode `yaml:"isolation"`

	InitialPrompt string `yaml:"initialPrompt"`

	OmitYukinoMd bool `yaml:"omitYukinoMd"`

	SystemPrompt string `yaml:"-"`

	FilePath string `yaml:"-"`
	Source   string `yaml:"-"`
	Filename string `yaml:"-"`
}

var validPermissionModes = map[string]bool{
	"":                  true,
	"acceptEdits":       true,
	"bypassPermissions": true,
	"default":           true,
	"plan":              true,
}

var validMemoryScopes = map[AgentMemoryScope]bool{
	"":                      true,
	AgentMemoryScopeUser:    true,
	AgentMemoryScopeProject: true,
	AgentMemoryScopeLocal:   true,
}

var validIsolationModes = map[IsolationMode]bool{
	"":                true,
	IsolationWorktree: true,
	IsolationRemote:   true,
}

func ParseAgentFile(path string) (*AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var def AgentDefinition
	def.FilePath = path

	fm, body, ok := frontmatter.Split(string(data))
	if ok {
		if err := yaml.Unmarshal([]byte(fm), &def); err != nil {
			return nil, fmt.Errorf("parse frontmatter in %s: %w", path, err)
		}
	}
	def.SystemPrompt = body

	if def.AgentType == "" {
		return nil, fmt.Errorf("agent definition %s: missing required field 'name'", path)
	}
	if def.WhenToUse == "" {
		return nil, fmt.Errorf("agent definition %s: missing required field 'description'", path)
	}

	def.Model = strings.TrimSpace(def.Model)
	if strings.EqualFold(def.Model, "inherit") {
		def.Model = "inherit"
	}

	if !validPermissionModes[def.PermissionMode] {
		return nil, fmt.Errorf("agent definition %s: invalid permissionMode '%s'", path, def.PermissionMode)
	}

	if !validMemoryScopes[def.Memory] {
		return nil, fmt.Errorf("agent definition %s: invalid memory scope '%s'", path, def.Memory)
	}

	if !validIsolationModes[def.Isolation] {
		return nil, fmt.Errorf("agent definition %s: invalid isolation mode '%s'", path, def.Isolation)
	}

	return &def, nil
}

func (d *AgentDefinition) ToSpec() SubAgentSpec {
	return SubAgentSpec{
		Name:                 d.AgentType,
		Description:          d.WhenToUse,
		Tools:                d.Tools,
		DisallowedTools:      d.DisallowedTools,
		SystemPromptOverride: d.SystemPrompt,
		MaxTurns:             d.MaxTurns,
		Model:                d.Model,
		PermissionMode:       d.PermissionMode,
		Background:           d.Background,
		Isolation:            d.Isolation,
		InitialPrompt:        d.InitialPrompt,
		OmitYukinoMd:         d.OmitYukinoMd,
		Skills:               d.Skills,
		Memory:               d.Memory,
		McpServers:           d.McpServers,
		RequiredMcpServers:   d.RequiredMcpServers,
		Hooks:                d.Hooks,
		Effort:               d.Effort,
	}
}

func (d *AgentDefinition) HasRequiredMcpServers(availableServers []string) bool {
	if len(d.RequiredMcpServers) == 0 {
		return true
	}
	for _, pattern := range d.RequiredMcpServers {
		patLower := strings.ToLower(pattern)
		matched := false
		for _, server := range availableServers {
			if strings.Contains(strings.ToLower(server), patLower) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
