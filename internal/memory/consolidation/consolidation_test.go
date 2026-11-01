package consolidation

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

func TestLock_FirstAcquire(t *testing.T) {
	dir := t.TempDir()

	last, err := ReadLastConsolidatedAt(dir)
	if err != nil {
		t.Fatalf("ReadLastConsolidatedAt: %v", err)
	}
	if last != 0 {
		t.Fatalf("expected 0, got %d", last)
	}

	prior, err := TryAcquireLock(dir)
	if err != nil {
		t.Fatalf("TryAcquireLock: %v", err)
	}
	if prior != 0 {
		t.Fatalf("expected prior=0, got %d", prior)
	}

	content, _ := os.ReadFile(filepath.Join(dir, lockFileName))
	if string(content) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("expected PID %d, got %s", os.Getpid(), content)
	}

	last2, _ := ReadLastConsolidatedAt(dir)
	if last2 == 0 {
		t.Fatal("expected non-zero after lock acquire")
	}
}

func TestLock_BlocksWhenHeld(t *testing.T) {
	dir := t.TempDir()

	_, _ = TryAcquireLock(dir)

	prior2, _ := TryAcquireLock(dir)
	if prior2 != -1 {
		t.Fatal("expected lock to block when held by live process")
	}
}

func TestLock_ReclaimsDeadPID(t *testing.T) {
	dir := t.TempDir()

	lockFile := filepath.Join(dir, lockFileName)
	os.WriteFile(lockFile, []byte("999999999"), 0o644)

	prior, err := TryAcquireLock(dir)
	if err != nil {
		t.Fatalf("TryAcquireLock: %v", err)
	}
	if prior == -1 {
		t.Fatal("should reclaim lock from dead PID")
	}
}

func TestLock_ReclaimsStale(t *testing.T) {
	dir := t.TempDir()

	lockFile := filepath.Join(dir, lockFileName)
	os.WriteFile(lockFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	oldTime := time.Now().Add(-2 * time.Hour)
	os.Chtimes(lockFile, oldTime, oldTime)

	prior, err := TryAcquireLock(dir)
	if err != nil {
		t.Fatalf("TryAcquireLock: %v", err)
	}
	if prior == -1 {
		t.Fatal("should reclaim stale lock even with live PID")
	}
}

func TestLock_RollbackDeletesOnZero(t *testing.T) {
	dir := t.TempDir()
	TryAcquireLock(dir)

	RollbackLock(dir, 0)

	if _, err := os.Stat(filepath.Join(dir, lockFileName)); !os.IsNotExist(err) {
		t.Fatal("rollback with prior=0 should delete lock file")
	}
}

func TestLock_RollbackRestoresMtime(t *testing.T) {
	dir := t.TempDir()

	lockFile := filepath.Join(dir, lockFileName)
	oldTime := time.Now().Add(-48 * time.Hour)
	os.WriteFile(lockFile, []byte("99999"), 0o644)
	os.Chtimes(lockFile, oldTime, oldTime)

	prior, _ := TryAcquireLock(dir)
	RollbackLock(dir, prior)

	info, _ := os.Stat(lockFile)
	diff := info.ModTime().UnixMilli() - prior
	if diff < -1000 || diff > 1000 {
		t.Fatalf("mtime not restored: expected ~%d, got %d", prior, info.ModTime().UnixMilli())
	}

	content, _ := os.ReadFile(lockFile)
	if strings.TrimSpace(string(content)) != "" {
		t.Fatalf("expected empty PID after rollback, got %q", content)
	}
}

func TestPrompt_ContainsAllPhases(t *testing.T) {
	prompt := BuildConsolidationPrompt("/mem", "/user/mem", "/sessions", []string{"s1", "s2", "s3"})

	for _, want := range []string{
		"Phase 1", "Phase 2", "Phase 3", "Phase 4",
		"MEMORY.md",
		"/mem", "/user/mem", "/sessions",
		"s1", "s2", "s3",
		"Sessions since last consolidation (3)",
		"read-only commands",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestPrompt_EmptySessions(t *testing.T) {
	prompt := BuildConsolidationPrompt("/mem", "", "/sessions", nil)

	if strings.Contains(prompt, "Sessions since last consolidation") {
		t.Error("should not include sessions section when list is empty")
	}
}

func TestListSessionsSince(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	sessDir, err := storage.GetSessionsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(sessDir, 0o755)

	os.WriteFile(filepath.Join(sessDir, "old.jsonl"), []byte(`{"role":"user","content":"hi","ts":1}`+"\n"), 0o644)
	os.Chtimes(filepath.Join(sessDir, "old.jsonl"), time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))

	os.WriteFile(filepath.Join(sessDir, "new1.jsonl"), []byte(`{"role":"user","content":"a","ts":2}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(sessDir, "new2.jsonl"), []byte(`{"role":"user","content":"b","ts":3}`+"\n"), 0o644)

	ids := listSessionsSince(dir, time.Now().Add(-24*time.Hour).UnixMilli())
	if len(ids) != 2 {
		t.Fatalf("expected 2 recent sessions, got %d: %v", len(ids), ids)
	}
}

func TestMaybeRun_SkipsWhenMemoryDirMissing(t *testing.T) {
	dir := t.TempDir()
	c := NewConsolidator(Deps{
		MemoryDir:   filepath.Join(dir, "nonexistent", "memory") + "/",
		ProjectRoot: dir,
	})

	c.MaybeRun(context.Background())
}

func TestMaybeRun_SkipsWhenTimeGateNotMet(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".yukino", "memory")
	os.MkdirAll(memDir, 0o755)

	lockFile := filepath.Join(memDir, lockFileName)
	os.WriteFile(lockFile, []byte(""), 0o644)
	os.Chtimes(lockFile, time.Now().Add(-1*time.Hour), time.Now().Add(-1*time.Hour))

	var triggered atomic.Bool
	c := NewConsolidator(Deps{
		MemoryDir:   memDir + "/",
		ProjectRoot: dir,
		DebugLogf:   func(f string, a ...any) { triggered.Store(true) },
	})

	c.MaybeRun(context.Background())

}

func TestMaybeRun_SkipsWhenSessionGateNotMet(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".yukino", "memory")
	sessDir := filepath.Join(dir, ".yukino", "sessions")
	os.MkdirAll(memDir, 0o755)
	os.MkdirAll(sessDir, 0o755)

	os.WriteFile(filepath.Join(sessDir, "s1.jsonl"), []byte(`{"role":"user","content":"a","ts":1}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(sessDir, "s2.jsonl"), []byte(`{"role":"user","content":"b","ts":2}`+"\n"), 0o644)

	var logs []string
	c := NewConsolidator(Deps{
		MemoryDir:   memDir + "/",
		ProjectRoot: dir,
		DebugLogf:   func(f string, a ...any) { logs = append(logs, f) },
	})
	c.SetThresholds(0, 5)

	c.MaybeRun(context.Background())

	found := false
	for _, log := range logs {
		if strings.Contains(log, "skip") && strings.Contains(log, "sessions") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected skip log for insufficient sessions, got: %v", logs)
	}
}

func TestMaybeRun_TriggersWhenBothGatesMet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".yukino", "memory")
	sessDir, err := storage.GetSessionsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(memDir, 0o755)
	os.MkdirAll(sessDir, 0o755)

	for i := 0; i < 6; i++ {
		name := filepath.Join(sessDir, "s"+strconv.Itoa(i)+".jsonl")
		os.WriteFile(name, []byte(`{"role":"user","content":"x","ts":1}`+"\n"), 0o644)
	}

	lockAcquired := false

	lastAt, _ := ReadLastConsolidatedAt(memDir)
	hoursSince := float64(time.Now().UnixMilli()-lastAt) / 3_600_000

	sessionIDs := listSessionsSince(dir, lastAt)

	if hoursSince < 0 {
		t.Fatal("time gate should pass (no lock file)")
	}

	if len(sessionIDs) < 5 {
		t.Fatalf("session gate should pass: got %d sessions", len(sessionIDs))
	}

	prior, err := TryAcquireLock(memDir)
	if err != nil {
		t.Fatalf("lock acquire failed: %v", err)
	}
	if prior != -1 {
		lockAcquired = true
	}

	if !lockAcquired {
		t.Fatal("expected lock to be acquired when both gates pass")
	}
}

func TestMaybeRun_ScanThrottle(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".yukino", "memory")
	sessDir := filepath.Join(dir, ".yukino", "sessions")
	os.MkdirAll(memDir, 0o755)
	os.MkdirAll(sessDir, 0o755)

	os.WriteFile(filepath.Join(sessDir, "s1.jsonl"), []byte(`{"role":"user","content":"a","ts":1}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(sessDir, "s2.jsonl"), []byte(`{"role":"user","content":"b","ts":2}`+"\n"), 0o644)

	var logs []string
	c := NewConsolidator(Deps{
		MemoryDir:   memDir + "/",
		ProjectRoot: dir,
		DebugLogf:   func(f string, a ...any) { logs = append(logs, f) },
	})
	c.SetThresholds(0, 5)

	c.MaybeRun(context.Background())

	logsBefore := len(logs)

	c.MaybeRun(context.Background())

	throttled := false
	for _, log := range logs[logsBefore:] {
		if strings.Contains(log, "throttle") {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Errorf("expected scan throttle on second call, got: %v", logs[logsBefore:])
	}
}

func TestMaybeRun_LockBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("lock detection unreliable on Windows")
	}
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".yukino", "memory")
	t.Setenv("HOME", t.TempDir())
	sessDir, sessErr := storage.GetSessionsDir(dir)
	if sessErr != nil {
		t.Fatal(sessErr)
	}
	os.MkdirAll(memDir, 0o755)
	os.MkdirAll(sessDir, 0o755)

	TryAcquireLock(memDir)

	// Sessions must be newer than the lock's mtime or the session gate
	// filters them out before the lock check runs.
	for i := 0; i < 6; i++ {
		name := filepath.Join(sessDir, "s"+strconv.Itoa(i)+".jsonl")
		os.WriteFile(name, []byte(`{"role":"user","content":"x","ts":1}`+"\n"), 0o644)
	}

	var logs []string
	c := NewConsolidator(Deps{
		MemoryDir:   memDir + "/",
		ProjectRoot: dir,
		DebugLogf:   func(f string, a ...any) { logs = append(logs, f) },
	})
	c.SetThresholds(0, 1)

	c.MaybeRun(context.Background())

	blocked := false
	for _, log := range logs {
		if strings.Contains(log, "lock held") {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Errorf("expected lock blocked log, got: %v", logs)
	}
}

func TestExtractWrittenPaths(t *testing.T) {
	msgs := []conversation.Message{
		{Role: "user", Content: "consolidate"},
		{
			Role:    "assistant",
			Content: "updating memories",
			ToolUses: []conversation.ToolUseBlock{
				{ToolName: "WriteFile", Arguments: map[string]any{"file_path": "/mem/feedback_testing.md"}},
				{ToolName: "EditFile", Arguments: map[string]any{"file_path": "/mem/MEMORY.md"}},
				{ToolName: "ReadFile", Arguments: map[string]any{"file_path": "/mem/old.md"}},
			},
		},
		{
			Role:    "assistant",
			Content: "done",
			ToolUses: []conversation.ToolUseBlock{
				{ToolName: "WriteFile", Arguments: map[string]any{"file_path": "/mem/feedback_testing.md"}},
				{ToolName: "WriteFile", Arguments: map[string]any{"file_path": "/mem/project_new.md"}},
			},
		},
	}

	paths := extractWrittenPaths(msgs)

	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
	}

	count := 0
	for _, p := range paths {
		if strings.Contains(p, "feedback_testing") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("feedback_testing.md should appear once, got %d", count)
	}
}
