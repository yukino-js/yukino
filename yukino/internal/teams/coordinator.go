package teams

var CoordinatorAllowedTools = map[string]bool{
	"Agent":           true,
	"SendMessage":     true,
	"TaskStop":        true,
	"SyntheticOutput": true,
	"TeamDelete":      true,
	"TeamCreate":      true, "TeamList": true, "TodoWrite": true,
}

func IsCoordinatorTool(name string) bool {
	return CoordinatorAllowedTools[name]
}

func CoordinatorToolFilter(enabled bool) func(name string) bool {
	if !enabled {
		return nil
	}
	return IsCoordinatorTool
}

func CoordinatorActiveFn(enabled bool) func() bool {
	if !enabled {
		return nil
	}
	return func() bool { return true }
}
