package skills

import (
	"strings"
)

type SkillMeta struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	WhenToUse   string   `yaml:"when_to_use"`
	Tags        []string `yaml:"tags"`
	Mode        string   `yaml:"mode"`
	Model       string   `yaml:"model"`
	Context     string   `yaml:"context"`
	ForkContext string   `yaml:"fork_context"`
}

func (m SkillMeta) IsFork() bool {
	return m.Mode == "fork" || m.Context == "fork"
}

type Skill struct {
	Meta        SkillMeta
	PromptBody  string
	SourceDir   string
	IsDirectory bool
	BodyLoaded  bool
}

func (s *Skill) Render(args string) string {
	body := s.PromptBody
	if strings.Contains(body, "$ARGUMENTS") {
		return strings.ReplaceAll(body, "$ARGUMENTS", args)
	}
	if strings.TrimSpace(args) == "" {
		return body
	}
	return body + "\n\n## User Request\n\n" + args
}
