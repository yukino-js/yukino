package permissions

import (
	"os"
	"strings"
	"testing"

	"github.com/yukino-js/yukino/yukino/internal/storage"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

func TestMalformedAndSymlinkedRulesDeny(t *testing.T) {
	setupHome(t)
	dir := t.TempDir()
	checker := NewChecker(NewPathSandbox(dir), NewRuleEngine(dir), ModeBypass)
	path, _ := storage.ProjectPath(dir, PermissionsRulesFilename)
	for _, content := range []string{"[broken", "rule: object", "- rule: 42\n  effect: allow"} {
		if err := storage.WritePrivateFileSync(path, []byte(content), os.O_WRONLY|os.O_CREATE|os.O_TRUNC); err != nil {
			t.Fatal(err)
		}
		decision := checker.Check(&tools.ReadFileTool{}, map[string]any{"file_path": "file.txt"})
		if decision.Effect != Deny || !strings.Contains(decision.Reason, RulesParseError) {
			t.Fatal(decision)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+"-missing", path); err != nil {
		t.Fatal(err)
	}
	if decision := checker.Check(&tools.ReadFileTool{}, map[string]any{"file_path": "file.txt"}); decision.Effect != Deny {
		t.Fatal(decision)
	}
}
func TestDelegatedModesAndPlanRestoration(t *testing.T) {
	setupHome(t)
	dir := t.TempDir()
	parent := NewChecker(NewPathSandbox(dir), NewRuleEngine(dir), ModeDefault)
	child := parent.ForSubagent(dir, ModePlan)
	changes := 0
	unsubscribe := parent.SubscribeMode(func() { changes++ })
	defer unsubscribe()
	parent.SetMode(ModeAcceptEdits)
	if child.Mode() != ModeAcceptEdits {
		t.Fatal("parent edit permissions did not reach child")
	}
	child.SetMode(ModeDefault)
	if changes != 2 {
		t.Fatalf("shared mode listeners: %d", changes)
	}
	child.Teammate = true
	child.RequiresLeaderPlanApproval = true
	child.SetMode(ModePlan)
	parent.SetMode(ModeBypass)
	if child.Mode() != ModePlan {
		t.Fatal("leader approval requirement bypassed")
	}
	child.ReadOnly = true
	child.SetMode(ModeBypass)
	if child.Mode() != ModePlan {
		t.Fatal("read-only child bypassed")
	}
	parent.SetMode(ModeAcceptEdits)
	if parent.EnterPlanMode() != ModeAcceptEdits || parent.Mode() != ModePlan {
		t.Fatal("failed to enter plan")
	}
	if parent.ExitPlanMode("") != ModeAcceptEdits || parent.Mode() != ModeAcceptEdits {
		t.Fatal("failed to restore mode")
	}
	clone := parent.ForCwd(t.TempDir())
	clone.SetMode(ModeDefault)
	if parent.Mode() != ModeDefault {
		t.Fatal("cwd clone did not share mode")
	}
}
