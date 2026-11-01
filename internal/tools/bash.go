package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/sandbox"
)

var commandErrorThresholds = map[string]int{
	"grep": 2,
	"rg":   2,
	"diff": 2,
	"find": 2,
	"test": 2,
	"[":    2,
}

func interpretExitCode(command string, exitCode int) bool {
	if exitCode == 0 {
		return false
	}
	base := extractLastCommand(command)
	if threshold, ok := commandErrorThresholds[base]; ok {
		return exitCode >= threshold
	}
	return true
}

func extractLastCommand(command string) string {
	parts := strings.Split(command, "|")
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return ""
	}
	fields := strings.Fields(last)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}

func exitCodeHint(command string, exitCode int) string {
	base := extractLastCommand(command)
	switch base {
	case "grep", "rg":
		if exitCode == 1 {
			return "no matches found"
		}
	case "diff":
		if exitCode == 1 {
			return "files differ"
		}
	case "find":
		if exitCode == 1 {
			return "some directories were inaccessible"
		}
	case "test", "[":
		if exitCode == 1 {
			return "condition is false"
		}
	}
	return fmt.Sprintf("command failed with exit code %d", exitCode)
}

const maxTimeout = 600

type BashTool struct {
	WorkDir       string
	Sandbox       sandbox.Sandbox
	SandboxConfig sandbox.Config
}

func (t *BashTool) Name() string { return "Bash" }

func (t *BashTool) Description() string { return BashDescription }

func (t *BashTool) Category() ToolCategory { return CategoryCommand }

func (t *BashTool) IsConcurrencySafe(args map[string]any) bool {
	cmd, ok := args["command"].(string)
	return ok && IsSafeCommand(cmd)
}

func (t *BashTool) Schema() map[string]any {
	return map[string]any{
		"name":        t.Name(),
		"description": t.Description(),
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "Shell command to execute"},
				"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (max 600)", "default": 120},
			},
			"required": []string{"command"},
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	command, _ := args["command"].(string)
	if command == "" {
		return ToolResult{Output: "Error: command is required", IsError: true}
	}

	timeout := intArg(args, "timeout", 120)
	if timeout > maxTimeout {
		timeout = maxTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	actualCommand := command
	if t.Sandbox != nil && t.Sandbox.Available() {
		wrapped, err := t.Sandbox.Wrap(command, t.SandboxConfig)
		if err == nil {
			actualCommand = wrapped
		}
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", actualCommand)
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	if t.WorkDir != "" {
		cmd.Dir = t.WorkDir
	}

	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return ToolResult{Output: fmt.Sprintf("Error: command timed out after %ds", timeout), IsError: true}
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == nil {
			return ToolResult{Output: fmt.Sprintf("Error executing command: %s", err), IsError: true}
		}
	}

	var sb bytes.Buffer
	fmt.Fprintf(&sb, "$ %s\n", command)
	if combined.Len() > 0 {
		sb.Write(combined.Bytes())
		if combined.Bytes()[combined.Len()-1] != '\n' {
			sb.WriteByte('\n')
		}
	}
	if exitCode != 0 {
		fmt.Fprintf(&sb, "Exit code %d", exitCode)
		if !interpretExitCode(command, exitCode) {
			fmt.Fprintf(&sb, " (%s)", exitCodeHint(command, exitCode))
		}
	}

	return ToolResult{Output: sb.String(), IsError: false}
}
