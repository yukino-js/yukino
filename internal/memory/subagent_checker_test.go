package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

func TestNewSubAgentCheckerOpensMemoryDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	userMemoryDir := filepath.Join(home, ".yukino", "memory")

	chk := NewSubAgentChecker(projectRoot, userMemoryDir)

	if decision := chk.Sandbox.Check(filepath.Join(userMemoryDir, "MEMORY.md")); decision != nil {
		t.Errorf("user-level memory dir must be open, got: %s", decision.Reason)
	}
	if decision := chk.Sandbox.Check(filepath.Join(projectRoot, "a.txt")); decision != nil {
		t.Errorf("project paths must be open, got: %s", decision.Reason)
	}
	projectMemoryDir, err := storage.ProjectPath(projectRoot, "memory")
	if err != nil {
		t.Fatal(err)
	}
	if decision := chk.Sandbox.Check(filepath.Join(projectMemoryDir, "topic.md")); decision != nil {
		t.Errorf("project memory dir must be open, got: %s", decision.Reason)
	}
	// The temp dir itself is an allowed root, so pick a path outside both the
	// project and the OS temp dir.
	unrelated := filepath.Join(string(filepath.Separator), "unrelated-dir-for-yukino-test", "x.txt")
	if decision := chk.Sandbox.Check(unrelated); decision == nil {
		t.Error("unrelated directories outside the project must stay closed")
	}
}

func TestNewSubAgentCheckerCarriesProjectRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()

	chk := NewSubAgentChecker(projectRoot, "")

	want, err := storage.ProjectPath(projectRoot, permissions.PermissionsRulesFilename)
	if err != nil {
		t.Fatal(err)
	}
	if got := chk.RuleEngine.ProjectPath(); got != want {
		t.Errorf("ProjectPath = %q, want %q", got, want)
	}
	if chk.Mode != permissions.ModeBypass {
		t.Errorf("Mode = %q, want bypassPermissions", chk.Mode)
	}
	_ = os.TempDir()
}
