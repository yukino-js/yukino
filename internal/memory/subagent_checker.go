package memory

import "github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"

func NewSubAgentChecker(projectRoot, userMemoryDir string) *permissions.Checker {
	sandbox := permissions.NewPathSandbox(projectRoot, true)
	if userMemoryDir != "" {
		sandbox.AddRoot(userMemoryDir)
	}
	if projectMemoryDir := GetAutoMemPath(projectRoot); projectMemoryDir != "" {
		sandbox.AddRoot(projectMemoryDir)
	}
	return permissions.NewChecker(
		sandbox,
		permissions.NewRuleEngine(projectRoot),
		permissions.ModeBypass,
	)
}
