package llm

import (
	"context"
	"testing"

	"github.com/yukino-js/yukino/yukino/internal/conversation"
)

func TestOutputLimitIsScopedToOneRequest(t *testing.T) {
	for _, protocol := range []string{"anthropic", "openai", "openai-compat"} {
		t.Run(protocol, func(t *testing.T) {
			server := newCaptureServer(t, protocol)
			client, err := NewClient(testProvider(protocol, server.srv.URL, nil), "system")
			if err != nil {
				t.Fatal(err)
			}
			original := client.(OutputBudgetProvider).GetOutputBudget()
			conv := conversation.NewManager()
			conv.AddUserMessage("request")
			events, errs := client.Stream(WithMaxOutputTokens(context.Background(), 2048), conv, nil)
			for range events {
			}
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			field := "max_tokens"
			if protocol == "openai" {
				field = "max_output_tokens"
			}
			if protocol == "openai-compat" {
				field = "max_completion_tokens"
			}
			if got := server.last(t)[field]; got != float64(2048) {
				t.Fatalf("request output limit = %v", got)
			}
			if got := client.(OutputBudgetProvider).GetOutputBudget(); got != original {
				t.Fatalf("request changed shared client: %+v -> %+v", original, got)
			}
			server.request(t, client)
			if got := server.last(t)[field]; got != float64(original.MaxOutputTokens) {
				t.Fatalf("request override leaked into next call: %v", got)
			}
		})
	}
}
