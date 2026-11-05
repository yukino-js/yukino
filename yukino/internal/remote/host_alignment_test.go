package remote

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/agent"
)

type hostReference struct {
	Protocol []struct {
		Label      string          `json:"label"`
		Input      json.RawMessage `json:"input"`
		Dispatched bool            `json:"dispatched"`
	} `json:"protocol"`
	Stream struct {
		Events []struct {
			Type, Text, Thinking, Signature, ToolID, ToolName, Output, InputID, Reason, Message, StopReason string
			Args                                                                                            map[string]any
			IsError                                                                                         bool
			Elapsed                                                                                         float64
			Delay                                                                                           int
			Usage                                                                                           struct{ InputTokens, OutputTokens int }
			Error                                                                                           struct{ Message string }
		} `json:"events"`
		Messages []map[string]any `json:"messages"`
	} `json:"stream"`
}

func loadHostReference(t *testing.T) hostReference {
	t.Helper()
	data, err := os.ReadFile("testdata/host-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference hostReference
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	return reference
}

func TestWebSocketValidationMatchesReference(t *testing.T) {
	for _, item := range loadHostReference(t).Protocol {
		t.Run(item.Label, func(t *testing.T) {
			message, valid := parseClientMessage(item.Input)
			_, supported := wsDataSchemas[message.Type]
			switch message.Type {
			case "cancel", "ping", "model_list_request", "provider_list_request", "rewind_list_request", "background_shells":
				supported = true
			}
			if dispatched := valid && supported; dispatched != item.Dispatched {
				t.Fatalf("dispatch=%v, reference=%v; input=%s", dispatched, item.Dispatched, item.Input)
			}
		})
	}
}

func TestRemoteStreamMatchesReference(t *testing.T) {
	reference := loadHostReference(t)
	s := newBootstrapServer(t)
	s.ag.Checker.SandboxEnabled = false
	s.ag.Checker.SandboxAutoAllow = false
	client := dialClient(t, s)
	events := make(chan agent.AgentEvent, len(reference.Stream.Events))
	for _, event := range reference.Stream.Events {
		switch event.Type {
		case "stream_text":
			events <- agent.StreamText{Text: event.Text}
		case "thinking_text":
			events <- agent.ThinkingText{Text: event.Text}
		case "thinking_complete":
			events <- agent.ThinkingComplete{Thinking: event.Thinking, Signature: event.Signature}
		case "tool_use":
			events <- agent.ToolUseEvent{ToolID: event.ToolID, ToolName: event.ToolName, Args: event.Args}
		case "tool_result":
			events <- agent.ToolResultEvent{ToolID: event.ToolID, ToolName: event.ToolName, Output: event.Output, IsError: event.IsError, Elapsed: time.Duration(event.Elapsed * float64(time.Second))}
		case "steering_delivered":
			events <- agent.SteeringDelivered{Text: event.Text, InputID: event.InputID}
		case "turn_complete":
			events <- agent.TurnComplete{Turn: 999}
		case "usage":
			events <- agent.UsageEvent{InputTokens: event.Usage.InputTokens, OutputTokens: event.Usage.OutputTokens}
		case "retry":
			events <- agent.RetryEvent{Reason: event.Reason, Wait: time.Duration(event.Delay) * time.Millisecond}
		case "compact":
			events <- agent.CompactEvent{Message: event.Message}
		case "error":
			events <- agent.ErrorEvent{Message: event.Error.Message}
		case "loop_complete":
			events <- agent.LoopComplete{TotalTurns: 999, StopReason: event.StopReason}
		}
	}
	close(events)
	s.agentCh = events
	s.consumeAgentEvents()
	for i, expected := range reference.Stream.Messages {
		actual := readJSON(t, client)
		if actual["type"] == "loop_complete" {
			actual["data"].(map[string]any)["elapsed"] = "<elapsed>"
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("message %d\nactual: %#v\nreference: %#v", i, actual, expected)
		}
	}
}
