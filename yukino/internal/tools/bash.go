package tools

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"

	"github.com/yukino-js/yukino/yukino/internal/sandbox"
)

type shellState struct {
	mu         sync.Mutex
	next       uint64
	foreground map[uint64]chan string
}

func (s *shellState) hasForeground() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.foreground) > 0
}
func (s *shellState) backgroundForeground() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, request := range s.foreground {
		select {
		case request <- "user":
			count++
		default:
		}
	}
	return count
}

type BashTool struct {
	WorkDir         string
	Sandbox         sandbox.Sandbox
	SandboxRequired bool
	SandboxConfig   sandbox.Config
	TaskManager     BackgroundTaskManager
	state           shellState
}

func (t *BashTool) Name() string           { return "Bash" }
func (t *BashTool) Description() string    { return BashDescription }
func (t *BashTool) Category() ToolCategory { return CategoryCommand }
func (t *BashTool) IsConcurrencySafe(args map[string]any) bool {
	command, ok := args["command"].(string)
	return ok && IsSafeCommand(command)
}
func (t *BashTool) HasForegroundTasks() bool       { return t.state.hasForeground() }
func (t *BashTool) BackgroundForegroundTasks() int { return t.state.backgroundForeground() }
func (t *BashTool) Schema() map[string]any {
	return shellSchema(t.Name(), t.Description(), "Shell command to execute", BashBackgroundDescription, t.TaskManager != nil)
}
func shellSchema(name, description, commandDescription, backgroundDescription string, background bool) map[string]any {
	properties := map[string]any{"command": map[string]any{"type": "string", "description": commandDescription}, "timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (max 600)", "default": 120}}
	if background {
		properties["run_in_background"] = map[string]any{"type": "boolean", "default": false, "description": "Run the command in the background. Returns a task ID immediately; the result arrives later as a task notification."}
		description += "\n" + backgroundDescription
	}
	return map[string]any{"name": name, "description": description, "input_schema": map[string]any{"type": "object", "properties": properties, "required": []string{"command"}}}
}
func (t *BashTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	return executeShell(ctx, t.Name(), "$ ", args, t.TaskManager, &t.state, t.WorkDir, func(command, cwd, outputDirectory string) (sandbox.PreparedCommand, error) {
		prepared := sandbox.PreparedCommand{Executable: "bash", Args: []string{"-c", command}}
		if !t.SandboxRequired && t.Sandbox == nil {
			return prepared, nil
		}
		if t.Sandbox == nil {
			return prepared, fmt.Errorf("sandbox is enabled but unavailable; command was not executed")
		}
		if !t.Sandbox.Available() {
			return prepared, fmt.Errorf("%s sandbox is unavailable; command was not executed", t.Sandbox.Implementation())
		}
		cfg := t.SandboxConfig
		runtime, _ := RuntimeContext(ctx)
		if runtime.Worktrees != nil {
			cfg.AllowWrite = runtime.Worktrees.SandboxWriteRoots(cfg.AllowWrite)
		}
		cfg.AllowWrite = append(append([]string(nil), cfg.AllowWrite...), outputDirectory)
		prepared, err := t.Sandbox.Prepare(ctx, command, cfg, cwd)
		if err != nil {
			return prepared, err
		}
		if runtime.Worktrees != nil {
			if dir := runtime.Worktrees.SandboxTempDirectory(); dir != "" {
				prepared.Env = append(append(os.Environ(), prepared.Env...), "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
			}
		}
		return prepared, nil
	})
}
func shellTimeout(args map[string]any) (int, bool) {
	value, ok := args["timeout"]
	if !ok {
		return 120, true
	}
	var number float64
	switch n := value.(type) {
	case float64:
		number = n
	case int:
		number = float64(n)
	case int64:
		number = float64(n)
	default:
		return 120, true
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 {
		return 0, false
	}
	return min(600, max(1, int(number))), true
}

func (t *BashTool) Dispose() error {
	var err error
	if disposable, ok := t.Sandbox.(interface{ Dispose() error }); ok {
		err = disposable.Dispose()
	}
	t.Sandbox = nil
	return err
}
