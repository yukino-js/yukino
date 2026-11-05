package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/tools"
	"github.com/yukino-js/yukino/yukino/internal/workflow"
)

func TestM18ReferenceRenderers(t *testing.T) {
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(before)
	var fixtures struct {
		Markdown []struct {
			Text           string
			Width          int
			Kind, Expected string
		}
		Previews []struct {
			Name, Text string
			Width      int
			Expected   string
		}
		Wraps []struct {
			Text        string
			Width       int
			Hard, Words []string
		}
		Tables []struct {
			Rows           [][]string
			Width, Columns int
			Fitted         *struct {
				ColumnWidths []int
				Rows         [][]string
			}
			Expected *string
		}
		Stable []struct{ Text, Expected string }
	}
	data, err := os.ReadFile("testdata/m18-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for i, x := range fixtures.Markdown {
		got := plainTerminalText(renderMarkdownText(x.Text, x.Width, x.Kind, false))
		if got != x.Expected {
			t.Errorf("markdown %d w=%d %s input=%q\n got %q\nwant %q", i, x.Width, x.Kind, x.Text, got, x.Expected)
		}
	}
	for i, x := range fixtures.Previews {
		if got := plainTerminalText(formatToolOutputPreview(x.Name, x.Text, x.Width)); got != x.Expected {
			t.Errorf("preview %d got %q want %q", i, got, x.Expected)
		}
	}
	for i, x := range fixtures.Wraps {
		if got := wrapToLines(x.Text, x.Width); !reflect.DeepEqual(got, x.Hard) {
			t.Errorf("hard wrap %d got %#v want %#v", i, got, x.Hard)
		}
		if got := wrapWordsToLines(x.Text, x.Width); !reflect.DeepEqual(got, x.Words) {
			t.Errorf("word wrap %d got %#v want %#v", i, got, x.Words)
		}
	}
	for i, x := range fixtures.Tables {
		rows, widths, ok := fitTableToWidth(x.Rows, x.Columns, x.Width)
		if ok != (x.Fitted != nil) {
			t.Errorf("table %d fit mismatch", i)
			continue
		}
		if ok {
			if !reflect.DeepEqual(rows, x.Fitted.Rows) || !reflect.DeepEqual(widths, x.Fitted.ColumnWidths) {
				t.Errorf("table %d fit got %#v %#v want %#v", i, rows, widths, x.Fitted)
			}
			if got := plainTerminalText(renderTable(rows, widths)); got != *x.Expected {
				t.Errorf("table %d got %q want %q", i, got, *x.Expected)
			}
		}
	}
	for i, x := range fixtures.Stable {
		if got := stableStreamingPrefix(x.Text); got != x.Expected {
			t.Errorf("stable %d prefix length %d want %d", i, len(got), len(x.Expected))
		}
	}
}

func TestToolResultsMatchIDAndCommitInCallOrder(t *testing.T) {
	m := newChatModel(t)
	for _, ev := range []agent.AgentEvent{
		agent.ToolUseEvent{ToolID: "one", ToolName: "Bash", Args: map[string]any{"command": "first"}},
		agent.ToolUseEvent{ToolID: "two", ToolName: "Bash", Args: map[string]any{"command": "second"}},
		agent.ToolResultEvent{ToolID: "two", ToolName: "Bash", Output: "second output", Elapsed: time.Second},
		agent.ToolResultEvent{ToolID: "one", ToolName: "Bash", Output: "first output", IsError: true},
	} {
		next, _ := m.handleAgentEvent(ev)
		m = asModel(t, next)
	}
	if m.toolBlocks[0].output != "first output" || m.toolBlocks[1].output != "second output" {
		t.Fatalf("results mismatched: %#v", m.toolBlocks)
	}
	if len(m.chatMessages) != 0 {
		t.Fatal("tools committed before the API turn completed")
	}
	next, _ := m.handleAgentEvent(agent.TurnComplete{})
	m = asModel(t, next)
	if len(m.chatMessages) != 1 || len(m.chatMessages[0].toolGroup) != 2 || len(m.toolBlocks) != 0 {
		t.Fatalf("tools not committed together: %#v", m.chatMessages)
	}
}

func TestThinkingAndToolCardsPreserveContent(t *testing.T) {
	m := newChatModel(t)
	for _, ev := range []agent.AgentEvent{
		agent.ThinkingText{Text: "reasoning"},
		agent.StreamText{Text: "answer"},
		agent.ToolUseEvent{ToolID: "read", ToolName: "ReadFile", Args: map[string]any{"file_path": "a.go"}},
		agent.ToolResultEvent{ToolID: "read", ToolName: "ReadFile", Output: "source body"},
		agent.TurnComplete{},
	} {
		next, _ := m.handleAgentEvent(ev)
		m = asModel(t, next)
	}
	if len(m.chatMessages) != 3 || m.chatMessages[0].content != "reasoning" || m.chatMessages[1].content != "answer" {
		t.Fatalf("thinking and answer lost: %#v", m.chatMessages)
	}
	collapsed := plainTerminalText(m.renderMessagesRange(0, 3))
	if !strings.Contains(collapsed, "Thinking · Ctrl+O details") || !strings.Contains(collapsed, "source body") {
		t.Fatalf("collapsed lost output: %q", collapsed)
	}
	m.toolsExpanded = true
	expanded := plainTerminalText(m.renderMessagesRange(0, 3))
	if !strings.Contains(expanded, "reasoning") {
		t.Fatalf("expanded thinking lost: %q", expanded)
	}
	for width := 1; width <= 100; width++ {
		m.width = width
		for _, line := range strings.Split(m.renderMessagesRange(0, 3), "\n") {
			if visibleWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
	}
}

func normalizeComponent(text string) string {
	lines := strings.Split(plainTerminalText(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
func TestM18ReferenceComponents(t *testing.T) {
	var fixtures struct {
		Cards []struct {
			Width int
			Props struct {
				ToolName, ArgsSummary, Output, Status, Progress string
				Elapsed                                         float64
			}
			Expected string
		}
		Thinking []struct {
			Width int
			Props struct {
				Text                string
				Duration            float64
				Expanded, Streaming bool
			}
			Expected string
		}
		Dialogs []struct {
			Kind        string
			Width, Rows int
			Expected    string
		}
	}
	data, err := os.ReadFile("testdata/m18-components.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for i, x := range fixtures.Cards {
		m := newChatModel(t)
		m.width = x.Width
		p := x.Props
		tb := toolBlockInfo{toolName: p.ToolName, args: map[string]any{"command": p.ArgsSummary}, output: p.Output, status: p.Status, progress: p.Progress, elapsed: p.Elapsed}
		got := normalizeComponent(m.renderToolBlock(tb))
		if got != x.Expected {
			t.Errorf("card %d width %d\ngot  %q\nwant %q", i, x.Width, got, x.Expected)
		}
	}
	for i, x := range fixtures.Thinking {
		m := newChatModel(t)
		m.width = x.Width
		m.toolsExpanded = x.Props.Expanded
		got := normalizeComponent(m.renderThinking(x.Props.Text, x.Props.Duration, x.Props.Streaming))
		if got != x.Expected {
			t.Errorf("thinking %d width %d\ngot  %q\nwant %q", i, x.Width, got, x.Expected)
		}
	}
	lookup := map[string]string{}
	for _, x := range fixtures.Dialogs {
		lookup[fmt.Sprintf("%s:%d:%d", x.Kind, x.Width, x.Rows)] = x.Expected
	}
	for _, kind := range []string{"thinking", "permission", "ask", "agents", "workflows"} {
		for _, width := range []int{1, 2, 5, 12, 24, 40, 80} {
			for _, height := range []int{3, 6, 10, 16, 24} {
				m := newChatModel(t)
				m.width = width
				m.height = height
				var actual string
				switch kind {
				case "thinking":
					m.thinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}
					m.thinkingCursor = 4
					actual = m.renderThinkingDialog()
				case "permission":
					m.enqueuePermission(agent.PermissionRequestEvent{ToolName: "Bash", Cwd: "/project", Args: map[string]any{"command": "go test"}, Decision: permissions.Decision{Reason: "Approval required", RememberPatterns: []string{"Bash(go test*)"}}})
					actual = m.renderPermDialog()
				case "ask":
					m.enqueueAsk(tools.AskUserRequest{Questions: []tools.Question{{Text: "Choose a color", Options: []tools.QuestionOption{{Label: "Blue", Description: "Sky color"}, {Label: "Green", Description: "Grass color"}}}}})
					actual = m.renderAskUserDialog()
				case "agents":
					actual = m.renderAgentEntries([]agentDialogEntry{{id: "task:agent-1", label: "agent-1: Inspect files", summary: "background subagent · running"}})

				case "workflows":
					actual = m.renderWorkflowsDialog()
				}
				key := fmt.Sprintf("%s:%d:%d", kind, width, m.dialogAvailableRows())
				expected, ok := lookup[key]
				if !ok {
					t.Fatalf("missing reference %s", key)
				}
				if got := normalizeComponent(actual); got != expected {
					t.Errorf("%s height %d\ngot  %q\nwant %q", key, height, got, expected)
				}
			}
		}
	}
}

func TestMessageLayoutsMatchReference(t *testing.T) {
	var fixture struct {
		Messages []struct {
			Width    int
			Expanded bool
			Message  struct{ Role, Content string }
			Expected string
		}
		Run       workflow.WorkflowRun
		Workflows []struct {
			Width, Rows int
			Expected    string
		}
	}
	data, err := os.ReadFile("testdata/message-layout-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for i, c := range fixture.Messages {
		m := newChatModel(t)
		m.width, m.toolsExpanded = c.Width, c.Expanded
		got := normalizeComponent(m.renderMessage(chatMessage{role: c.Message.Role, content: c.Message.Content}))
		if got != c.Expected {
			t.Errorf("message %d width %d expanded %v\ngot  %q\nwant %q", i, c.Width, c.Expanded, got, c.Expected)
		}
	}
	for _, c := range fixture.Workflows {
		m := newChatModel(t)
		m.width = c.Width
		for height := c.Rows; height <= c.Rows+20; height++ {
			m.height = height
			if m.dialogAvailableRows() == c.Rows {
				break
			}
		}
		if m.dialogAvailableRows() != c.Rows {
			t.Fatalf("cannot allocate %d dialog rows", c.Rows)
		}
		service := workflow.AttachWorkflow(workflow.WorkflowServiceOptions{WorkflowBackendOptions: workflow.WorkflowBackendOptions{Registry: m.registry}})
		service.Store.Set(&fixture.Run)
		got := normalizeComponent(m.renderWorkflowsDialog())
		if got != c.Expected {
			t.Errorf("workflow width %d rows %d\ngot  %q\nwant %q", c.Width, c.Rows, got, c.Expected)
		}
	}
}
