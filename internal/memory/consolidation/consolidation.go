package consolidation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/memory"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/subagent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

const (
	defaultMinHours    = 24
	defaultMinSessions = 5
	scanThrottleMs     = 10 * 60 * 1000
)

type Deps struct {
	MemoryDir     string
	UserMemoryDir string
	ProjectRoot   string
	Client        llm.Client
	ToolRegistry  *tools.Registry
	Protocol      string
	Conversation  *conversation.Manager
	AppendSystem  func(string)
	DebugLogf     func(string, ...any)
}

type Consolidator struct {
	deps        Deps
	lastScanAt  int64
	minHours    int
	minSessions int
}

func NewConsolidator(deps Deps) *Consolidator {
	return &Consolidator{
		deps:        deps,
		minHours:    defaultMinHours,
		minSessions: defaultMinSessions,
	}
}

func (c *Consolidator) SetThresholds(minHours, minSessions int) {
	c.minHours = minHours
	c.minSessions = minSessions
}

func (c *Consolidator) MaybeRun(ctx context.Context) {
	if c == nil {
		return
	}
	if _, err := os.Stat(strings.TrimRight(c.deps.MemoryDir, string(filepath.Separator))); os.IsNotExist(err) {
		return
	}

	lastAt, err := ReadLastConsolidatedAt(c.deps.MemoryDir)
	if err != nil {
		c.debugf("[consolidation] ReadLastConsolidatedAt failed: %v", err)
		return
	}
	hoursSince := float64(time.Now().UnixMilli()-lastAt) / 3_600_000
	if hoursSince < float64(c.minHours) {
		return
	}

	now := time.Now().UnixMilli()
	if now-c.lastScanAt < scanThrottleMs {
		c.debugf("[consolidation] scan throttle — last scan %ds ago", (now-c.lastScanAt)/1000)
		return
	}
	c.lastScanAt = now

	sessionIDs := listSessionsSince(c.deps.ProjectRoot, lastAt)
	if len(sessionIDs) < c.minSessions {
		c.debugf("[consolidation] skip — %d sessions since last consolidation, need %d",
			len(sessionIDs), c.minSessions)
		return
	}

	priorMtime, err := TryAcquireLock(c.deps.MemoryDir)
	if err != nil {
		c.debugf("[consolidation] lock acquire failed: %v", err)
		return
	}
	if priorMtime == -1 {
		c.debugf("[consolidation] lock held by another process")
		return
	}

	c.debugf("[consolidation] firing — %.1fh since last, %d sessions to review",
		hoursSince, len(sessionIDs))

	go c.run(ctx, sessionIDs, priorMtime)
}

func (c *Consolidator) run(ctx context.Context, sessionIDs []string, priorMtime int64) {
	defer func() {
		if r := recover(); r != nil {
			c.debugf("[consolidation] panic: %v", r)
			RollbackLock(c.deps.MemoryDir, priorMtime)
		}
	}()

	transcriptDir, err := storage.GetSessionsDir(c.deps.ProjectRoot)
	if err != nil {
		c.debugf("[consolidation] cannot resolve sessions dir: %v", err)
		return
	}
	prompt := BuildConsolidationPrompt(
		c.deps.MemoryDir, c.deps.UserMemoryDir,
		transcriptDir, sessionIDs,
	)

	conv := conversation.NewManager()
	conv.AddUserMessage(prompt)

	subRegistry := subagent.FilterToolsForAgent(c.deps.ToolRegistry, nil, nil, true)

	subChecker := memory.NewSubAgentChecker(c.deps.ProjectRoot, c.deps.UserMemoryDir)

	subAgent := agent.New(c.deps.Client, subRegistry, c.deps.Protocol)
	subAgent.MaxIterations = 15
	subAgent.Checker = subChecker
	subAgent.WorkDir = c.deps.ProjectRoot

	startTime := time.Now()
	ch := subAgent.Run(ctx, conv)
	for range ch {
	}

	writtenPaths := extractWrittenPaths(conv.GetMessages())
	c.debugf("[consolidation] finished in %s, %d files touched: %v",
		time.Since(startTime), len(writtenPaths), writtenPaths)

	var memoryPaths []string
	for _, p := range writtenPaths {
		if filepath.Base(p) == memory.AutoMemEntrypointName {
			continue
		}
		memoryPaths = append(memoryPaths, p)
	}

	if len(memoryPaths) > 0 && c.deps.AppendSystem != nil {
		var names []string
		for _, p := range memoryPaths {
			names = append(names, filepath.Base(p))
		}
		c.deps.AppendSystem(fmt.Sprintf("Memory improved: %s", strings.Join(names, ", ")))
	}
}

func listSessionsSince(projectRoot string, sinceMs int64) []string {
	sessions := session.ListSessions(projectRoot)
	since := time.UnixMilli(sinceMs)
	var ids []string
	for _, s := range sessions {
		if s.ModTime.After(since) {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

func extractWrittenPaths(messages []conversation.Message) []string {
	var paths []string
	seen := make(map[string]struct{})
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tu := range m.ToolUses {
			if tu.ToolName != "WriteFile" && tu.ToolName != "EditFile" {
				continue
			}
			fp, ok := tu.Arguments["file_path"].(string)
			if !ok || fp == "" {
				continue
			}
			if _, exists := seen[fp]; exists {
				continue
			}
			seen[fp] = struct{}{}
			paths = append(paths, fp)
		}
	}
	return paths
}

func (c *Consolidator) debugf(format string, args ...any) {
	if c.deps.DebugLogf != nil {
		c.deps.DebugLogf(format, args...)
	}
}
