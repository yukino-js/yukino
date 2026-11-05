package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/history"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

func initializedHost(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m := newChatModel(t)
	m.activateInitialProvider(m.selectedProvider)
	if m.ag == nil {
		t.Fatal("host initialization failed")
	}
	t.Cleanup(func() {
		if err := m.Dispose(); err != nil {
			t.Error(err)
		}
	})
	return m
}

func TestHostClearResetsConversationState(t *testing.T) {
	m := initializedHost(t)
	oldSession := m.sessionID
	cachePath := filepath.Join(t.TempDir(), "read.txt")
	m.ag.FileStateCache.Record(cachePath, 1)
	m.ag.RecordRecentTool("ReadFile")
	m.ag.MarkMemoriesSurfaced([]string{"memory.md"})
	m.ag.Checker.SetPlanFilePath("old-plan.md")
	m.ag.ActivateSkill("active", "skill body")
	m.pendingMessages = []queuedMessage{{text: "old queue"}}
	next, _ := m.executeCommand("/clear", nil)
	m = asModel(t, next)
	if m.sessionID == oldSession || m.ag.Checker.CurrentPlanPath() != "" || m.ag.FileStateCache.Has(cachePath) || len(m.pendingMessages) != 0 {
		t.Fatal("clear retained prior session state")
	}
	if recent, surfaced := m.ag.RecallHints(); len(recent) != 0 || len(surfaced) != 0 {
		t.Fatal("recall state survived clear")
	}
	if m.ag.GetActiveSkills()["active"] != "skill body" {
		t.Fatal("clear dropped the app's active skill cache")
	}
}

func TestHostResumeKeepsExcludedBashCard(t *testing.T) {
	m := initializedHost(t)
	result := conversation.UserBashResult{Command: "echo private", Output: "private", ExcludeFromContext: true, Status: "completed"}
	saved := []session.Message{{Role: "user", Content: conversation.UserBashContext(result), UserBash: &result}}
	next, _ := m.doResumeSession(m.getWorkDir(), session.NewID(), saved)
	m = asModel(t, next)
	if len(m.chatMessages) != 2 || len(m.chatMessages[0].toolGroup) != 1 || m.chatMessages[0].toolGroup[0].progress != UserBashProgressExcluded {
		t.Fatalf("excluded Bash card was not restored: %#v", m.chatMessages)
	}
	for _, message := range m.conversation.GetMessages() {
		if strings.Contains(message.ContentText(), "private") {
			t.Fatal("excluded Bash output entered model context")
		}
	}
}

func TestHostForkSkillIsForegroundAndCancelable(t *testing.T) {
	m := initializedHost(t)
	root := t.TempDir()
	directory := filepath.Join(root, ".agents", "skills", "fork-test")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: fork-test\ndescription: fork test\nmode: fork\n---\nRun task"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.skillCatalog = skills.LoadCatalog(root)
	bootstrapCount := len(m.chatMessages)
	next, cmd := m.startSkillFork("fork-test", "arguments")
	m = asModel(t, next)
	if !m.streaming || !m.foregroundBusy() || m.cancelStream == nil {
		t.Fatal("fork skill did not own foreground execution")
	}
	if len(m.chatMessages) != bootstrapCount+1 || m.chatMessages[bootstrapCount].role != "system" || m.chatMessages[bootstrapCount].content != "Running skill \"fork-test\" in fork mode…" {
		t.Fatal("fork notice differs from the reference")
	}
	m.interruptForeground()
	commands := cmd().(tea.BatchMsg)
	finished := commands[len(commands)-1]().(forkSkillDoneMsg)
	if !errors.Is(finished.err, context.Canceled) {
		t.Fatalf("fork did not receive cancellation: %v", finished.err)
	}
	next, _ = m.update(finished)
	m = asModel(t, next)
	if m.streaming || m.cancelStream != nil {
		t.Fatal("fork completion retained foreground ownership")
	}
}

func TestHostLocalCommandsRecordHistoryOnce(t *testing.T) {
	m := initializedHost(t)
	next, _ := m.dispatchInput("/status", nil)
	m = asModel(t, next)
	entries, err := history.LoadInputs()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Text != "/status" || entries[0].CommandName == nil || *entries[0].CommandName != "status" {
		t.Fatalf("command history = %#v", entries)
	}
}

func TestHostQueuedAskCancellationDoesNotWaitForHead(t *testing.T) {
	m := newChatModel(t)
	first, second := make(chan tools.QuestionResponse, 1), make(chan tools.QuestionResponse, 1)
	ctx, cancel := context.WithCancel(context.Background())
	m.enqueueAsk(tools.AskUserRequest{Questions: []tools.Question{{Text: "first"}}, ResponseCh: first})
	m.enqueueAsk(tools.AskUserRequest{Context: ctx, Questions: []tools.Question{{Text: "second"}}, ResponseCh: second})
	cancel()
	m.pruneInteractions()
	select {
	case answer := <-second:
		if len(answer.Answers) != 0 {
			t.Fatal(answer)
		}
	default:
		t.Fatal("canceled queued ask is still waiting behind the active dialog")
	}
	if !m.askUserDialog || m.askUserRespCh != first {
		t.Fatal("canceling a queued ask displaced the active dialog")
	}
	m.denyAllInteractions()
}
