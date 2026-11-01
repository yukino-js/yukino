//go:build unix

package tools_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/sandbox"
	"github.com/yukino-js/yukino/yukino/internal/subagent"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

type unavailableSandbox struct{}

func (unavailableSandbox) Implementation() string { return "test" }
func (unavailableSandbox) Available() bool        { return false }
func (unavailableSandbox) Prepare(context.Context, string, sandbox.Config, string) (sandbox.PreparedCommand, error) {
	panic("unavailable sandbox prepared")
}
func TestShellErrorsCancellationTimeoutAndSandbox(t *testing.T) {
	dir := t.TempDir()
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{WorkDir: dir})
	bash := &tools.BashTool{}
	result := bash.Execute(ctx, map[string]any{"command": "exit 7"})
	if !result.IsError || !strings.Contains(result.Output, "Exit code 7") {
		t.Fatal(result)
	}
	for _, tool := range []tools.Tool{bash, &tools.PowerShellTool{}} {
		if result := tool.Execute(ctx, map[string]any{"command": "true", "timeout": 0}); !result.IsError || !strings.Contains(result.Output, "greater than 0") {
			t.Fatal(result)
		}
	}
	bash.SandboxRequired = true
	bash.Sandbox = unavailableSandbox{}
	if result := bash.Execute(ctx, map[string]any{"command": "touch unsafe"}); !result.IsError || !strings.Contains(result.Output, "command was not executed") {
		t.Fatal(result)
	}
	if _, err := os.Stat(filepath.Join(dir, "unsafe")); !os.IsNotExist(err) {
		t.Fatal("sandbox fell through")
	}
	bash.Sandbox = nil
	bash.SandboxRequired = false
	outputSeen := make(chan struct{}, 1)
	cancelled, cancel := context.WithCancel(tools.WithExecutionContext(context.Background(), tools.ExecutionContext{WorkDir: dir, OnOutput: func(output string) {
		if strings.Contains(output, "before") {
			select {
			case outputSeen <- struct{}{}:
			default:
			}
		}
	}}))
	resultCh := make(chan tools.ToolResult, 1)
	go func() { resultCh <- bash.Execute(cancelled, map[string]any{"command": "printf before; sleep 10"}) }()
	select {
	case <-outputSeen:
		cancel()
	case <-time.After(4 * time.Second):
		cancel()
		t.Fatal("output observer did not fire")
	}
	select {
	case result := <-resultCh:
		if !result.IsError || !strings.Contains(result.Output, "before") || !strings.Contains(result.Output, "command interrupted") {
			t.Fatal(result)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("process tree did not stop")
	}
	result = bash.Execute(ctx, map[string]any{"command": "printf deadline; sleep 5", "timeout": 1})
	if !result.IsError || !strings.Contains(result.Output, "deadline") || !strings.Contains(result.Output, "timed out after 1s") {
		t.Fatal(result)
	}
}
func TestShellBackgroundTaskOutputAndStop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	manager := subagent.NewTaskManager()
	bash := &tools.BashTool{TaskManager: manager}
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{WorkDir: dir, SessionID: "shell-test"})
	result := bash.Execute(ctx, map[string]any{"command": "printf before; sleep 20", "run_in_background": true})
	if result.IsError || !strings.Contains(result.Output, "running in background") {
		t.Fatal(result)
	}
	tasks := manager.List()
	if len(tasks) != 1 || tasks[0].Kind != "shell" || !strings.HasPrefix(tasks[0].ID, "bash-") {
		t.Fatal(tasks)
	}
	id := tasks[0].ID
	output := (&tools.TaskOutputTool{TaskManager: manager}).Execute(ctx, map[string]any{"task_id": id, "wait": true, "timeout_ms": 0})
	if output.IsError || !strings.Contains(output.Output, `"timed_out": true`) {
		t.Fatal(output)
	}
	if _, err := manager.StopAndWait(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if task := manager.Get(id); task.Status != "cancelled" {
		t.Fatal(task)
	}
	if len(manager.DrainNotifications()) != 1 {
		t.Fatal("missing cancellation notification")
	}
}
func TestShellLargeOutputHelper(t *testing.T) {
	if os.Getenv("YUKINO_SHELL_TEST_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			n, _ := strconv.Atoi(os.Args[i+1])
			fmt.Print(strings.Repeat("あ", n))
			os.Exit(7)
		}
	}
	os.Exit(1)
}
func TestShellLargeOutputSpillsWithoutLoading(t *testing.T) {
	t.Setenv("YUKINO_SHELL_TEST_HELPER", "1")
	t.Setenv("HOME", t.TempDir())
	command := "exec '" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "' -test.run=^TestShellLargeOutputHelper$ -- 4000000"
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{WorkDir: t.TempDir(), SessionID: "large-output"})
	result := (&tools.BashTool{}).Execute(ctx, map[string]any{"command": command})
	if !result.IsError || !strings.Contains(result.Output, "Exit code 7") || !strings.Contains(result.Output, "Preview (first 1998 bytes)") || len(result.Output) > 10000 {
		t.Fatal(result)
	}
	marker := "Full content saved to:\n"
	start := strings.Index(result.Output, marker)
	if start < 0 {
		t.Fatal(result)
	}
	path := strings.SplitN(result.Output[start+len(marker):], "\n", 2)[0]
	info, err := os.Stat(path)
	if err != nil || info.Size() != 12000000 {
		t.Fatal(info, err)
	}
}
