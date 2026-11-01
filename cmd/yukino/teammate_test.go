package main

import (
	"context"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/teams"
)

func TestParseTeammateFlagsAbsent(t *testing.T) {
	cases := [][]string{
		{},
		{"--help"},
		{"--something", "else"},
	}
	for _, args := range cases {
		if _, ok := parseTeammateFlags(args); ok {
			t.Errorf("parseTeammateFlags(%v) returned ok=true, want false", args)
		}
	}
}

func TestParseTeammateFlagsBasic(t *testing.T) {
	args := []string{"--teammate", "--team-name", "alpha", "--agent-name", "ann"}
	got, ok := parseTeammateFlags(args)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.teamName != "alpha" || got.memberName != "ann" {
		t.Errorf("parsed = %+v", got)
	}
}

func TestParseTeammateFlagsMissingValue(t *testing.T) {
	args := []string{"--teammate", "--team-name"}
	got, ok := parseTeammateFlags(args)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.teamName != "" {
		t.Errorf("expected empty teamName, got %q", got.teamName)
	}
}

func TestParseTeammateFlagsIgnoresUnknown(t *testing.T) {
	args := []string{"--teammate", "--noise", "x", "--team-name", "t", "--agent-name", "m"}
	got, ok := parseTeammateFlags(args)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.teamName != "t" || got.memberName != "m" {
		t.Errorf("parsed = %+v", got)
	}
}

func TestBuildTeammateRegistryToolSet(t *testing.T) {
	reg := buildTeammateRegistry(context.Background(), teammateToolOptions{
		WorkDir:    t.TempDir(),
		Protocol:   "anthropic",
		SessionID:  "sess-1",
		TeamMgr:    teams.NewTeamManager(t.TempDir()),
		TeamName:   "alpha",
		MemberName: "ann",
	})

	mustHave := []string{
		"ReadFile", "WriteFile", "EditFile", "Bash", "Glob", "Grep",
		"ToolSearch", "SyntheticOutput", "EnterWorktree", "ExitWorktree",
		"SendMessage", "TodoWrite", "LSP", "WebFetch",
	}
	for _, name := range mustHave {
		if reg.Get(name) == nil {
			t.Errorf("teammate tool set is missing %s", name)
		}
	}

	mustNotHave := []string{"Agent", "TeamCreate", "TeamDelete", "TaskCreate", "TaskGet", "TaskList", "TaskUpdate"}
	for _, name := range mustNotHave {
		if reg.Get(name) != nil {
			t.Errorf("teammate tool set must not contain %s", name)
		}
	}
}
