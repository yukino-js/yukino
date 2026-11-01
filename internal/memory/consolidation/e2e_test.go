//go:build e2e

package consolidation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/memory"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

func TestE2E_ConsolidationMergesDuplicates(t *testing.T) {
	apiKey := os.Getenv("YUKINO_TEST_API_KEY")
	baseURL := os.Getenv("YUKINO_TEST_BASE_URL")
	model := os.Getenv("YUKINO_TEST_MODEL")
	if apiKey == "" {
		t.Skip("YUKINO_TEST_API_KEY not set, skipping E2E test")
	}
	if baseURL == "" {
		baseURL = "https://api.minimaxi.com/v1"
	}
	if model == "" {
		model = "MiniMax-M3"
	}

	dir := t.TempDir()
	memDir := filepath.Join(dir, ".yukino", "memory")
	os.MkdirAll(memDir, 0o755)

	writeMemory(t, memDir, "feedback_no_push.md", "feedback", "no-push",
		"Don't push without asking",
		"用户不希望自动 push 代码")

	writeMemory(t, memDir, "feedback_auto_push.md", "feedback", "auto-push",
		"Don't auto push code",
		"用户不喜欢自动 push，每次都要先问一下")

	writeMemory(t, memDir, "project_deadline.md", "project", "deadline",
		"Project deadline is 2025-01-15",
		"项目截止日期是 2025 年 1 月 15 日\n\n**Why:** 客户要求\n**How to apply:** 所有任务在此之前完成")

	writeMemory(t, memDir, "user_role.md", "user", "user-role",
		"User is a backend engineer",
		"用户是后端工程师，主要用 Go 和 Java")

	indexContent := `- [No push](feedback_no_push.md) — 不要自动 push
- [Auto push](feedback_auto_push.md) — 不要自动 push 代码
- [Deadline](project_deadline.md) — 项目截止日期 2025-01-15
- [User role](user_role.md) — 后端工程师
`
	os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte(indexContent), 0o644)

	t.Logf("Test directory: %s", dir)
	t.Logf("Memory files before consolidation:")
	listMemoryFiles(t, memDir)

	cfg := &config.ProviderConfig{}
	cfg.Protocol = "openai-compat"
	cfg.BaseURL = baseURL
	cfg.APIKey = apiKey
	cfg.Model = model
	cfg.ContextWindow = 200000
	client, err := llm.NewClient(cfg, "")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	registry := tools.NewRegistry()
	registry.Register(&tools.ReadFileTool{})
	registry.Register(&tools.WriteFileTool{})
	registry.Register(&tools.EditFileTool{})
	registry.Register(&tools.GlobTool{})
	registry.Register(&tools.GrepTool{})
	registry.Register(&tools.BashTool{})
	subRegistry := subagent.FilterToolsForAgent(registry, nil, nil, true)

	sandbox := permissions.NewPathSandbox(memDir)
	checker := permissions.NewChecker(sandbox, &permissions.RuleEngine{}, permissions.ModeBypass)

	prompt := BuildConsolidationPrompt(memDir, "", "", nil)

	conv := conversation.NewManager()
	conv.AddUserMessage(prompt)

	subAgent := agent.New(client, subRegistry, "openai")
	subAgent.MaxIterations = 15
	subAgent.Checker = checker
	subAgent.WorkDir = dir

	t.Log("Starting consolidation sub-agent...")
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ch := subAgent.Run(ctx, conv)
	for event := range ch {
		switch e := event.(type) {
		case agent.StreamText:
			fmt.Print(e.Text)
		case agent.LoopComplete:
			t.Logf("\nSub-agent completed in %s, %d turns", time.Since(startTime), e.TotalTurns)
		}
	}

	t.Log("\n\nMemory files after consolidation:")
	listMemoryFiles(t, memDir)

	files := listMdFiles(memDir)
	hasPush := false
	pushCount := 0
	for _, f := range files {
		content, _ := os.ReadFile(filepath.Join(memDir, f))
		if strings.Contains(strings.ToLower(string(content)), "push") {
			pushCount++
			hasPush = true
		}
	}
	if hasPush && pushCount > 1 {
		t.Logf("WARNING: push-related memories not merged (still %d files)", pushCount)
	} else if pushCount <= 1 {
		t.Log("PASS: duplicate push memories merged")
	}

	indexBytes, err := os.ReadFile(filepath.Join(memDir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("MEMORY.md not found after consolidation")
	}
	t.Logf("MEMORY.md content:\n%s", string(indexBytes))

	lines := strings.Split(strings.TrimSpace(string(indexBytes)), "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > memory.MaxEntrypointLines {
		t.Errorf("MEMORY.md has %d lines, exceeds limit %d", nonEmpty, memory.MaxEntrypointLines)
	}

	writtenPaths := extractWrittenPaths(conv.GetMessages())
	t.Logf("Files written by sub-agent: %v", writtenPaths)
	if len(writtenPaths) == 0 {
		t.Error("sub-agent did not write any files — consolidation had no effect")
	}
}

func writeMemory(t *testing.T, dir, filename, memType, name, desc, body string) {
	t.Helper()
	content := fmt.Sprintf(`---
name: %s
description: %s
metadata:
  type: %s
---

%s
`, name, desc, memType, body)
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func listMemoryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			info, _ := e.Info()
			t.Logf("  %s (%d bytes)", e.Name(), info.Size())
		}
	}
}

func listMdFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") && e.Name() != "MEMORY.md" {
			files = append(files, e.Name())
		}
	}
	return files
}
