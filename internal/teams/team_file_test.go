package teams

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTeamFileRoundTrip(t *testing.T) {
	useTempHome(t)

	tm := NewTeamManager(t.TempDir())
	team := tm.CreateTeamFull("Refactor Auth", ModeInProcess, "lead", "refactor the auth module")
	team.AddMember("alice", nil, nil, "anthropic")
	team.SetMemberMeta("alice", "worker", "claude-sonnet-4-6", "/tmp/wt/alice")

	fresh := NewTeamManager(tm.cwd)
	got := fresh.GetTeam("Refactor Auth")
	if got == nil {
		t.Fatal("expected the team to be rebuilt from disk, got nil")
	}
	if got.LeadAgentID != "lead" {
		t.Errorf("LeadAgentID = %q, want lead", got.LeadAgentID)
	}
	if got.Description != "refactor the auth module" {
		t.Errorf("Description = %q, want refactor the auth module", got.Description)
	}
	m, ok := got.Members["alice"]
	if !ok {
		t.Fatalf("member alice was not restored, members: %v", got.Members)
	}
	if m.AgentType != "worker" || m.Model != "claude-sonnet-4-6" || m.WorktreePath != "/tmp/wt/alice" {
		t.Errorf("member metadata mismatch: %+v", m)
	}
}

func TestTeamFilePathIsSanitized(t *testing.T) {
	useTempHome(t)

	cwd := t.TempDir()
	tm := NewTeamManager(cwd)
	tm.CreateTeamFull("Refactor Auth!", ModeTmux, "lead", "")

	want := filepath.Join(TeamsBaseDir(cwd), "refactor-auth-", "config.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected the config at %s, stat failed: %v", want, err)
	}
}

func TestDeleteTeamRemovesDir(t *testing.T) {
	useTempHome(t)

	cwd := t.TempDir()
	tm := NewTeamManager(cwd)
	tm.CreateTeamFull("gone", ModeInProcess, "lead", "")
	if _, err := os.Stat(teamDir(cwd, "gone")); err != nil {
		t.Fatalf("the directory must exist after creating the team: %v", err)
	}

	tm.DeleteTeam("gone")
	if _, err := os.Stat(teamDir(cwd, "gone")); !os.IsNotExist(err) {
		t.Errorf("the directory must be removed after deleting the team, err = %v", err)
	}
	if fresh := NewTeamManager(cwd).GetTeam("gone"); fresh != nil {
		t.Errorf("a deleted team must not be recoverable from disk")
	}
}

func TestGetTeamMissingReturnsNil(t *testing.T) {
	useTempHome(t)
	if got := NewTeamManager(t.TempDir()).GetTeam("never-existed"); got != nil {
		t.Errorf("a nonexistent team should return nil, got %+v", got)
	}
}
