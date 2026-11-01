package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/conversation"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

const openaiCompatStreamIdleTimeout = 5 * time.Minute

type openaiCompatClient struct {
	client          openai.Client
	model           string
	cfg             config.ProviderConfig
	thinkingLevel   string
	systemPrompt    string
	maxOutputTokens int
}

func newOpenAICompatClient(cfg *config.ProviderConfig, systemPrompt string) (*openaiCompatClient, error) {
	apiKey := config.ResolveAPIKey(cfg)
	if apiKey == "" {
		return nil, &AuthenticationError{Message: OpenAIAPIKeyMissingError}
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(cfg.BaseURL),
	}
	if os.Getenv("YUKINO_LLM_DEBUG") != "" {
		opts = append(opts, option.WithDebugLog(log.New(os.Stderr, "[llm] ", log.LstdFlags)))
	}
	client := openai.NewClient(opts...)

	return &openaiCompatClient{
		client:          client,
		model:           cfg.Model,
		cfg:             *cfg,
		thinkingLevel:   config.GetThinkingLevel(cfg),
		systemPrompt:    systemPrompt,
		maxOutputTokens: config.GetMaxOutputTokens(cfg),
	}, nil
}

func (c *openaiCompatClient) SetSystemPrompt(prompt string) {
	c.systemPrompt = prompt
}

func (c *openaiCompatClient) SetMaxOutputTokens(tokens int) {
	c.maxOutputTokens = tokens
}

func (c *openaiCompatClient) SetThinkingLevel(level string) string {
	c.thinkingLevel = config.ClampThinkingLevel(&c.cfg, level)
	return c.thinkingLevel
}

func (c *openaiCompatClient) GetThinkingLevel() string {
	return c.thinkingLevel
}

func (c *openaiCompatClient) GetSupportedThinkingLevels() []string {
	return config.GetSupportedThinkingLevels(&c.cfg)
}

func (c *openaiCompatClient) GetOutputBudget() OutputBudget {
	return OutputBudget{
		MaxOutputTokens: c.maxOutputTokens,
		ThinkingTokens:  config.ThinkingBudgetForProvider(&c.cfg, c.thinkingLevel),
	}
}

type compatToolCall struct {
	id      string
	name    string
	args    string
	started bool
}

func (c *openaiCompatClient) Stream(ctx context.Context, conv *conversation.Manager, toolSchemas []map[string]any) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 64)
	errs := make(chan error, 1)

	messages := buildChatCompletionMessages(c.systemPrompt, conversation.EnsureToolPairing(conv.GetMessages()))

	var tools []openai.ChatCompletionToolParam
	for _, schema := range toolSchemas {
		s, _ := schema["function"].(map[string]any)
		name, _ := s["name"].(string)
		desc, _ := s["description"].(string)
		params, _ := s["parameters"].(map[string]any)
		tools = append(tools, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        name,
				Description: param.NewOpt(desc),
				Parameters:  shared.FunctionParameters(params),
				Strict:      param.NewOpt(false),
			},
		})
	}

	go func() {
		defer close(events)
		defer close(errs)

		requestConfig := c.cfg
		requestConfig.MaxOutputTokens = float64(maxOutputTokens(ctx, c.maxOutputTokens))
		reqParams := openai.ChatCompletionNewParams{
			Model:               c.model,
			Messages:            messages,
			MaxCompletionTokens: param.NewOpt(int64(config.GetMaxOutputTokens(&requestConfig))),
			StreamOptions: openai.ChatCompletionStreamOptionsParam{
				IncludeUsage: param.NewOpt(true),
			},
		}
		if len(tools) > 0 {
			reqParams.Tools = tools
		}
		// Configured non-reasoning providers omit the field; off otherwise
		// sends none explicitly so a server default cannot silently enable
		// reasoning.
		if effort := config.ToReasoningEffort(c.GetThinkingLevel(), &c.cfg); effort != "" {
			reqParams.ReasoningEffort = shared.ReasoningEffort(effort)
		}

		stream := c.client.Chat.Completions.NewStreaming(ctx, reqParams)
		defer stream.Close()

		toolCalls := map[int64]*compatToolCall{}
		var toolCallOrder []int64
		var reasoningAccum string
		finishReason := ""
		sawFinish := false
		var inputTokens, outputTokens, cacheReadTokens int

		type sseResult struct {
			hasNext bool
		}
		nextCh := make(chan sseResult, 1)

		readNext := func() {
			nextCh <- sseResult{hasNext: stream.Next()}
		}

		idle := time.NewTimer(openaiCompatStreamIdleTimeout)
		defer idle.Stop()

		finish := func(err error) {
			errs <- err
		}

		go readNext()
		for {
			var res sseResult
			select {
			case <-ctx.Done():
				finish(&NetworkError{Message: fmt.Sprintf("context cancelled: %v", ctx.Err())})
				return
			case <-idle.C:
				finish(&NetworkError{Message: fmt.Sprintf("stream idle timeout: no SSE events for %s", openaiCompatStreamIdleTimeout)})
				return
			case res = <-nextCh:
			}

			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(openaiCompatStreamIdleTimeout)

			if !res.hasNext {
				break
			}

			chunk := stream.Current()

			// Usage may arrive in a trailing chunk with empty choices, so
			// check it before the delta guard.
			if chunk.JSON.Usage.Valid() {
				outputTokens = int(chunk.Usage.CompletionTokens)
				cacheReadTokens = int(chunk.Usage.PromptTokensDetails.CachedTokens)
				inputTokens = int(chunk.Usage.PromptTokens) - cacheReadTokens
				if inputTokens < 0 {
					inputTokens = 0
				}
			}

			if len(chunk.Choices) == 0 {
				go readNext()
				continue
			}
			choice := chunk.Choices[0]
			delta := choice.Delta

			if delta.Content != "" {
				events <- TextDelta{Text: delta.Content}
			}

			if rc, ok := delta.JSON.ExtraFields["reasoning_content"]; ok && rc.Valid() {
				raw := rc.Raw()
				if len(raw) >= 2 && raw[0] == '"' {
					var text string
					if json.Unmarshal([]byte(raw), &text) == nil && text != "" {
						reasoningAccum += text
						events <- ThinkingDelta{Text: text}
					}
				}
			}

			for _, tc := range delta.ToolCalls {
				entry, exists := toolCalls[tc.Index]
				if !exists {
					entry = &compatToolCall{}
					toolCalls[tc.Index] = entry
					toolCallOrder = append(toolCallOrder, tc.Index)
				}
				if tc.ID != "" {
					entry.id = tc.ID
				}
				if tc.Function.Name != "" {
					entry.name = tc.Function.Name
				}
				// Emit start as soon as both id and name are known: compat
				// gateways sometimes deliver the id only in a later delta, and
				// consumers must never see deltas/complete for a call that
				// never "started".
				if !entry.started && entry.id != "" && entry.name != "" {
					entry.started = true
					events <- ToolCallStart{ToolName: entry.name, ToolID: entry.id}
				}
				if tc.Function.Arguments != "" {
					entry.args += tc.Function.Arguments
					if entry.started {
						events <- ToolCallDelta{Text: tc.Function.Arguments}
					}
				}
			}

			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
				sawFinish = true
				if reasoningAccum != "" {
					events <- ThinkingComplete{Thinking: reasoningAccum}
					reasoningAccum = ""
				}
				for _, index := range toolCallOrder {
					call := toolCalls[index]
					if call.started {
						continue
					}
					missing := ""
					if call.id == "" {
						missing = "id"
					}
					if call.name == "" {
						if missing != "" {
							missing += " and "
						}
						missing += "name"
					}
					finish(&NetworkError{Message: fmt.Sprintf("Chat Completions tool call ended without a valid %s", missing)})
					return
				}
				for _, index := range toolCallOrder {
					call := toolCalls[index]
					args, parseError := ParseToolArguments(call.args)
					events <- ToolCallComplete{
						ToolID:     call.id,
						ToolName:   call.name,
						Arguments:  args,
						ParseError: parseError,
					}
				}
			}

			go readNext()
		}

		if err := stream.Err(); err != nil {
			finish(classifyOpenAIError(err))
			return
		}

		if !sawFinish {
			finish(&NetworkError{Message: "Chat Completions stream ended before a finish reason"})
			return
		}

		// Map the raw finish_reason explicitly. content_filter is a blocked
		// response, not a normal completion, and must fail even when tool
		// deltas arrived — never run pending tools from an invalid stop state.
		// Unknown reasons are surfaced rather than silently treated as
		// end_turn.
		var stopReason string
		switch {
		case finishReason == "content_filter":
			finish(&LLMError{Message: "Chat Completions response was blocked by the content filter"})
			return
		case finishReason == "length":
			stopReason = "max_tokens"
		case finishReason == "tool_calls" || finishReason == "function_call" || (finishReason == "stop" && len(toolCalls) > 0):
			stopReason = "tool_use"
		case finishReason == "stop":
			stopReason = "end_turn"
		default:
			finish(&LLMError{Message: fmt.Sprintf("Chat Completions returned an unsupported finish reason: %s", finishReason)})
			return
		}

		events <- StreamEnd{
			StopReason: stopReason,
			Usage: UsageInfo{
				InputTokens:     inputTokens,
				OutputTokens:    outputTokens,
				CacheReadTokens: cacheReadTokens,
			},
		}
	}()

	return events, errs
}

func buildChatCompletionMessages(systemPrompt string, messages []conversation.Message) []openai.ChatCompletionMessageParamUnion {
	var result []openai.ChatCompletionMessageParamUnion

	if systemPrompt != "" {
		result = append(result, openai.SystemMessage(systemPrompt))
	}

	for _, m := range messages {
		if m.Role == "assistant" {
			var reasoning string
			for _, tb := range m.ThinkingBlocks {
				reasoning += tb.Thinking
			}

			if len(m.ToolUses) > 0 {
				assistant := openai.ChatCompletionAssistantMessageParam{}
				if m.Content != "" {
					assistant.Content.OfString = param.NewOpt(m.Content)
				}
				for _, tu := range m.ToolUses {
					argsJSON, _ := json.Marshal(tu.Arguments)
					assistant.ToolCalls = append(assistant.ToolCalls, openai.ChatCompletionMessageToolCallParam{
						ID: tu.ToolUseID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tu.ToolName,
							Arguments: string(argsJSON),
						},
					})
				}
				if reasoning != "" {
					assistant.SetExtraFields(map[string]any{"reasoning_content": reasoning})
				}
				result = append(result, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
			} else if m.Content != "" || reasoning != "" {
				assistant := openai.ChatCompletionAssistantMessageParam{}
				if m.Content != "" {
					assistant.Content.OfString = param.NewOpt(m.Content)
				}
				if reasoning != "" {
					assistant.SetExtraFields(map[string]any{"reasoning_content": reasoning})
				}
				result = append(result, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
			}
		} else if len(m.ToolResults) > 0 {
			for _, tr := range m.ToolResults {
				result = append(result, openai.ToolMessage(tr.Content, tr.ToolUseID))
			}
			if m.Content != "" {
				result = append(result, openai.UserMessage(m.Content))
			}
		} else {
			result = append(result, openai.UserMessage(m.Content))
		}
	}

	return result
}
