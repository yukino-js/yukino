package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/images"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
)

func cleanFrame(text string) string {
	lines := strings.Split(stripANSI(text), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func TestM17ReferenceFixtures(t *testing.T) {
	var fixture struct {
		Reference  string
		Cwd        string
		InputCases []struct {
			Lines []string
			Width int
			Paste bool
			Rows  []struct {
				Line, Width int
				Cells       []struct {
					Offset, Width int
					Text          string
				}
			}
			Cursor struct{ Row, Cell, Column int }
		}
		BorderCases []struct {
			Width    int
			Status   string
			Hidden   int
			Expected string
		}
		FooterCases []struct {
			Width, Rows int
			Expected    string
		}
		ChromeCases []struct {
			Rows     int
			Subtitle bool
			Expected struct {
				Borders, Title, Hint, Detail bool
				Height                       int
			}
		}
	}
	data, err := os.ReadFile("testdata/m17-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Reference != "1847a71e03f90d0e5fc60a2a797c60af327b7750" {
		t.Fatal("reference baseline changed")
	}
	for i, tc := range fixture.InputCases {
		t.Run(fmt.Sprintf("input/%d", i), func(t *testing.T) {
			var store *pasteStore
			if tc.Paste {
				_, store = collapsePaste(strings.Repeat("x", 1001), nil)
			}
			actual := layoutInputRows(tc.Lines, tc.Width, store)
			if len(actual) != len(tc.Rows) {
				t.Fatalf("rows %d != reference %d", len(actual), len(tc.Rows))
			}
			for j, row := range actual {
				expected := tc.Rows[j]
				if row.line != expected.Line || row.width != expected.Width || len(row.cells) != len(expected.Cells) {
					t.Fatalf("row %d: %+v != reference %+v", j, row, expected)
				}
				for k, cell := range row.cells {
					want := expected.Cells[k]
					if cell.offset != want.Offset || cell.width != want.Width || cell.text != want.Text {
						t.Fatalf("cell %d/%d: %+v != reference %+v", j, k, cell, want)
					}
				}
			}
			pos := locateInputCursor(actual, len(tc.Lines)-1, len([]rune(tc.Lines[len(tc.Lines)-1])))
			if pos.row != tc.Cursor.Row || pos.cell != tc.Cursor.Cell || pos.column != tc.Cursor.Column {
				t.Fatalf("cursor %+v != reference %+v", pos, tc.Cursor)
			}
		})
	}
	for i, tc := range fixture.BorderCases {
		t.Run(fmt.Sprintf("border/%d", i), func(t *testing.T) {
			if got := cleanFrame(statusBorder(tc.Width, tc.Status, "⠋", tc.Hidden, false)); got != tc.Expected {
				t.Fatalf("got %q, reference %q", got, tc.Expected)
			}
		})
	}
	info := footerData{model: "reference-model", provider: "reference-provider", mode: "default", thinking: "high", cwd: fixture.Cwd, sessionID: "12345678-1234-1234-1234-123456789012", inputTokens: 12345, outputTokens: 6789, contextWindow: 200000}
	for i, tc := range fixture.FooterCases {
		t.Run(fmt.Sprintf("footer/%d", i), func(t *testing.T) {
			if got := cleanFrame(renderFooter(info, tc.Width, tc.Rows)); got != tc.Expected {
				t.Fatalf("got %q, reference %q", got, tc.Expected)
			}
		})
	}
	for _, tc := range fixture.ChromeCases {
		got := selectorChrome(tc.Rows, tc.Subtitle)
		want := tc.Expected
		if got.borders != want.Borders || got.title != want.Title || got.hint != want.Hint || got.detail != want.Detail || got.height != want.Height {
			t.Errorf("chrome(%d,%v) = %+v, reference %+v", tc.Rows, tc.Subtitle, got, want)
		}
	}
}

func keyModel(t *testing.T, m Model, key tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(key)
	return asModel(t, updated), cmd
}
func TestComposerCompletesBeforeSubmitting(t *testing.T) {
	m := newChatModel(t)
	m.width = 80
	m.height = 24
	m.editor.SetValue("/cle")
	m.updateSlashMenu()
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.editor.Value() != "/clear " || len(m.chatMessages) > 0 {
		t.Fatalf("Enter must complete first: %q", m.editor.Value())
	}
	m.streaming = true
	m.editor.SetValue("follow-up")
	cancelled := false
	m.cancelStream = func() { cancelled = true }
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cancelled || !m.streaming || !reflect.DeepEqual(m.pendingMessages, []string{"follow-up"}) || m.editor.Value() != "" {
		t.Fatal("busy input was lost or interrupted the foreground")
	}
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.editor.Value() != "follow-up" || len(m.pendingMessages) != 0 {
		t.Fatal("Up did not recall the latest queued message")
	}
}
func TestComposerPasteAndHistoryPreserveDraft(t *testing.T) {
	m := newChatModel(t)
	m.width = 80
	m.height = 24
	m.editor.SetWidth(78)
	text := strings.Repeat("日\n", 11)
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	if m.editor.Value() != "[paste #1 +12 lines]" || expandPastes(m.editor.Value(), m.editor.pastes) != text {
		t.Fatal("paste did not collapse and round-trip")
	}
	draft := m.editor.snapshot()
	m.historyEntries = []string{"older"}
	m.historyUp()
	m.historyDown()
	if !reflect.DeepEqual(m.editor.snapshot(), draft) {
		t.Fatal("history traversal lost draft cursor or paste store")
	}
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.editor.Value() != "" {
		t.Fatalf("paste marker must delete as an atom: %q", m.editor.Value())
	}
	marker, store := collapseImage("'@image.png'", nil)
	if got := expandPastes("x"+marker+"y", store); got != "x '@image.png' y" {
		t.Fatalf("attachment boundaries = %q", got)
	}
	store.entries[marker] = "[Image #2]"
	if got := expandPastes(marker, store); got != "[Image #2]" {
		t.Fatalf("pasted content was recursively expanded: %q", got)
	}
}
func TestComposerVisualMovementAndGraphemeDelete(t *testing.T) {
	m := newChatModel(t)
	m.width = 6
	m.height = 24
	m.editor.SetWidth(4)
	m.editor.SetValue("abcdefghi")
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.editor.line != 0 || m.editor.col != 5 {
		t.Fatalf("wrapped Up = %d/%d", m.editor.line, m.editor.col)
	}
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.editor.col != 1 || m.editor.Value() != "abcdefghi" {
		t.Fatal("visual movement altered logical draft")
	}
	m.editor.SetValue("abc👨‍👩‍👧‍👦")
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.editor.Value() != "abc" {
		t.Fatalf("split emoji grapheme: %q", m.editor.Value())
	}
	m.editor.SetWidth(20)
	m.editor.SetValue("abcdef\nx\nabcdef")
	m.editor.col = 5
	if !m.editor.moveVertical(-1) || m.editor.col != 1 || !m.editor.moveVertical(-1) || m.editor.col != 5 {
		t.Fatal("preferred display column was lost")
	}
}
func TestComposerEscDismissesCompletionBeforeWork(t *testing.T) {
	m := newChatModel(t)
	m.width = 80
	m.height = 24
	m.streaming = true
	cancelled := false
	m.cancelStream = func() { cancelled = true }
	m.editor.SetValue("/cle")
	m.updateSlashMenu()
	m, _ = keyModel(t, m, escKey)
	if cancelled || m.slashMenuOpen {
		t.Fatal("Esc must dismiss completion before interrupting")
	}
	m, _ = keyModel(t, m, escKey)
	if !cancelled || m.streaming {
		t.Fatal("second Esc must interrupt foreground")
	}
}
func TestTerminalControlsExitAndModeCycle(t *testing.T) {
	m := newChatModel(t)
	m.editor.SetValue("draft")
	m, cmd := keyModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil || m.ctrlCDeadline.IsZero() || m.editor.Value() != "" {
		t.Fatal("first Ctrl+C must clear and show exit hint")
	}
	generation := m.ctrlCGeneration
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if !m.ctrlCDeadline.IsZero() {
		t.Fatal("editing must clear exit hint")
	}
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	m, cmd = keyModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("second Ctrl+C must quit")
	}
	m.ctrlCDeadline = time.Now().Add(time.Second)
	updated, _ := m.Update(ctrlCExpiredMsg{generation: generation})
	m = asModel(t, updated)
	if m.ctrlCDeadline.IsZero() {
		t.Fatal("stale timer cleared a newer hint")
	}
	checker := permissions.NewChecker(permissions.NewPathSandbox(t.TempDir(), true), permissions.NewRuleEngine(t.TempDir()), permissions.ModeDefault)
	m.ag = &agent.Agent{Checker: checker}
	for _, want := range []permissions.PermissionMode{permissions.ModeAcceptEdits, permissions.ModeBypass, permissions.ModeDefault} {
		m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
		if checker.Mode() != want {
			t.Fatalf("cycle = %s, want %s", checker.Mode(), want)
		}
	}
	checker.EnterPlanMode()
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if checker.Mode() != permissions.ModeDefault {
		t.Fatal("Shift+Tab must leave plan for Default")
	}
}
func TestClipboardGenerationAndSelectorInput(t *testing.T) {
	m := newChatModel(t)
	m.pasteGeneration = 2
	m.pastingImage = true
	m.handleClipboard(clipboardImageMsg{generation: 1, result: images.SaveClipboardImageResult{OK: true, Value: "/tmp/image.png"}})
	if m.editor.Value() != "" || !m.pastingImage {
		t.Fatal("stale clipboard completion changed the draft")
	}
	m.clearDraft()
	if m.pastingImage || m.editor.pastes != nil {
		t.Fatal("clear retained clipboard state")
	}
	for _, text := range []string{"\x1b[31m", "[<0;4;5M"} {
		if got := updateSelectorQuery("draft", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}); got != "draft" {
			t.Fatalf("terminal protocol entered query: %q", got)
		}
	}
	if got := updateSelectorQuery("", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\r\n\tb")}); got != "a b" {
		t.Fatalf("paste whitespace = %q", got)
	}
}
func TestComposerFrameFitsTerminalAndResizeKeepsDraft(t *testing.T) {
	for _, width := range []int{1, 2, 4, 10, 20, 40, 80, 120} {
		for _, height := range []int{2, 4, 6, 10, 24} {
			m := newChatModel(t)
			m.ready = true
			m.width = width
			m.height = height
			m.editor.SetWidth(max(1, width-boolColumn(width > 2)*2))
			m.editor.SetValue(strings.Repeat("日本👨‍👩‍👧‍👦é long ", 20) + "\nlast")
			before := m.editor.snapshot()
			frame := m.renderChatView()
			if lineCount(frame) > max(1, height-1) {
				t.Fatalf("%dx%d overflowed rows: %d", width, height, lineCount(frame))
			}
			for _, line := range strings.Split(frame, "\n") {
				if visibleWidth(line) > width {
					t.Fatalf("%dx%d overflowed width: %q", width, height, stripANSI(line))
				}
			}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width + 1, Height: height + 1})
			after := asModel(t, updated)
			if !reflect.DeepEqual(before, after.editor.snapshot()) {
				t.Fatal("resize changed stored draft")
			}
		}
	}
}
func TestQueueRunsAfterForegroundEndsAndWaitsForInitialization(t *testing.T) {
	m := newChatModel(t)
	m.editor.SetValue("draft typed during execution")
	m.ag = &agent.Agent{}
	m.pendingMessages = []string{"/help"}
	updated, _ := m.Update(struct{}{})
	m = asModel(t, updated)
	if m.editor.Value() != "draft typed during execution" {
		t.Fatal("queued dispatch cleared the current editor draft")
	}
	if len(m.pendingMessages) != 0 || len(m.chatMessages) != 1 {
		t.Fatal("idle follow-up was not dispatched")
	}
	m.ag = nil
	m.pendingMessages = []string{"not ready"}
	updated, _ = m.Update(struct{}{})
	m = asModel(t, updated)
	if len(m.pendingMessages) != 1 {
		t.Fatal("queue dispatched before agent initialization")
	}
}

func TestQueuedTurnWaitsForAgentCleanup(t *testing.T) {
	m := newChatModel(t)
	m.ag = &agent.Agent{}
	m.streaming = true
	ch := make(chan agent.AgentEvent)
	m.agentCh = ch
	m.pendingMessages = []string{"/help"}
	updated, _ := m.Update(agentEventMsg{ch: ch, event: agent.LoopComplete{}})
	m = asModel(t, updated)
	if len(m.pendingMessages) != 1 || m.settlingCh != ch || !m.settleWaiting {
		t.Fatal("follow-up ran before cleanup or cleanup waiter was lost")
	}
	close(ch)
	updated, _ = m.Update(foregroundSettledMsg{ch: ch})
	m = asModel(t, updated)
	if len(m.pendingMessages) != 0 || m.settlingCh != nil {
		t.Fatal("follow-up did not resume after cleanup")
	}
}
func TestModalDraftAndClipboardCommandState(t *testing.T) {
	m := newChatModel(t)
	m.editor.SetValue("hidden draft")
	m.providerDialog = true
	m, _ = keyModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.editor.Value() != "hidden draft" {
		t.Fatal("idle Ctrl+C discarded the unmounted composer draft")
	}
	m.providerDialog = false
	var cmd tea.Cmd
	pasteKey := tea.KeyMsg{Type: tea.KeyCtrlV}
	if runtime.GOOS == "windows" {
		pasteKey = tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("v")}
	}
	m, cmd = keyModel(t, m, pasteKey)
	if cmd == nil || !m.pastingImage || m.pasteGeneration == 0 {
		t.Fatal("clipboard command did not retain its pending state")
	}
	generation := m.pasteGeneration
	m.handleClipboard(clipboardImageMsg{generation: generation, result: images.SaveClipboardImageResult{OK: true, Value: "/tmp/image.png"}, cwd: "/tmp"})
	if !strings.Contains(m.editor.Value(), "[Image #1]") || m.pastingImage {
		t.Fatal("clipboard completion failed to insert attachment")
	}
}
