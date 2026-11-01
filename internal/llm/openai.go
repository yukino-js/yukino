package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

const openaiStreamIdleTimeout = 5 * time.Minute

type openaiClient struct {
	client          openai.Client
	model           string
	cfg             config.ProviderConfig
	thinkingLevel   string
	systemPrompt    string
	maxOutputTokens int
}

func newOpenAIClient(cfg *config.ProviderConfig, systemPrompt string) (*openaiClient, error) {
	apiKey := config.ResolveAPIKey(cfg)
	if apiKey == "" {
		return nil, &AuthenticationError{
			Message: OpenAIAPIKeyMissingError,
		}
	}

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(cfg.BaseURL),
	)

	return &openaiClient{
		client:          client,
		model:           cfg.Model,
		cfg:             *cfg,
		thinkingLevel:   config.GetThinkingLevel(cfg),
		systemPrompt:    systemPrompt,
		maxOutputTokens: config.GetMaxOutputTokens(cfg),
	}, nil
}

func (c *openaiClient) SetSystemPrompt(prompt string) {
	c.systemPrompt = prompt
}

func (c *openaiClient) SetMaxOutputTokens(tokens int) {
	c.maxOutputTokens = tokens
}

func (c *openaiClient) SetThinkingLevel(level string) string {
	c.thinkingLevel = config.ClampThinkingLevel(&c.cfg, level)
	return c.thinkingLevel
}

func (c *openaiClient) GetThinkingLevel() string {
	return c.thinkingLevel
}

func (c *openaiClient) GetSupportedThinkingLevels() []string {
	return config.GetSupportedThinkingLevels(&c.cfg)
}

func (c *openaiClient) GetOutputBudget() OutputBudget {
	return OutputBudget{
		MaxOutputTokens: c.maxOutputTokens,
		ThinkingTokens:  config.ThinkingBudgetForProvider(&c.cfg, c.thinkingLevel),
	}
}

func (c *openaiClient) Stream(ctx context.Context, conv *conversation.Manager, toolSchemas []map[string]any) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 64)
	errs := make(chan error, 1)

	input := buildOpenAIInput(conversation.EnsureToolPairing(conv.GetMessages()))

	var tools []responses.ToolUnionParam
	for _, s := range toolSchemas {
		name, _ := s["name"].(string)
		desc, _ := s["description"].(string)
		params, _ := s["parameters"].(map[string]any)
		tools = append(tools, responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        name,
				Description: param.NewOpt(desc),
				Parameters:  params,
				Strict:      param.NewOpt(false),
			},
		})
	}

	go func() {
		defer close(events)
		defer close(errs)

		reqParams := responses.ResponseNewParams{
			Model:        c.model,
			Instructions: param.NewOpt(c.systemPrompt),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: input,
			},
		}
		// Only explicit reasoning:false omits the field. Off sends none to
		// defeat server-default reasoning; requesting a summary while off is
		// unnecessary.
		if effort := config.ToReasoningEffort(c.GetThinkingLevel(), &c.cfg); effort != "" {
			reqParams.Reasoning = shared.ReasoningParam{
				Effort: shared.ReasoningEffort(effort),
			}
			if effort != "none" {
				reqParams.Reasoning.Summary = shared.ReasoningSummaryAuto
			}
		}
		if len(tools) > 0 {
			reqParams.Tools = tools
		}

		stream := c.client.Responses.NewStreaming(ctx, reqParams)
		defer stream.Close()

		var currentToolName, currentCallID, jsonAccum string
		var reasoningID, reasoningText string

		type sseResult struct {
			hasNext bool
		}
		nextCh := make(chan sseResult, 1)

		readNext := func() {
			nextCh <- sseResult{hasNext: stream.Next()}
		}

		idle := time.NewTimer(openaiStreamIdleTimeout)
		defer idle.Stop()

		go readNext()
		for {
			var res sseResult
			select {
			case <-ctx.Done():
				errs <- &NetworkError{Message: fmt.Sprintf("context cancelled: %v", ctx.Err())}
				return
			case <-idle.C:
				errs <- &NetworkError{Message: fmt.Sprintf("stream idle timeout: no SSE events for %s", openaiStreamIdleTimeout)}
				return
			case res = <-nextCh:
			}

			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(openaiStreamIdleTimeout)

			if !res.hasNext {
				break
			}

			event := stream.Current()
			switch event.Type {
			case "response.output_text.delta":
				events <- TextDelta{Text: event.Delta.OfString}
			case "response.output_item.added":
				switch event.Item.Type {
				case "function_call":
					currentToolName = event.Item.Name
					currentCallID = event.Item.CallID
					jsonAccum = ""
					events <- ToolCallStart{ToolName: currentToolName, ToolID: currentCallID}
				case "reasoning":
					reasoningID = event.Item.ID
					reasoningText = ""
				}
			case "response.reasoning_summary_text.delta":
				reasoningText += event.Delta.OfString
				events <- ThinkingDelta{Text: event.Delta.OfString}
			case "response.reasoning_summary_text.done":
				events <- ThinkingComplete{Thinking: reasoningText, Signature: reasoningID}
			case "response.function_call_arguments.delta":
				jsonAccum += event.Delta.OfString
				events <- ToolCallDelta{Text: event.Delta.OfString}
			case "response.function_call_arguments.done":
				args, parseError := ParseToolArguments(jsonAccum)
				events <- ToolCallComplete{
					ToolID:     currentCallID,
					ToolName:   currentToolName,
					Arguments:  args,
					ParseError: parseError,
				}
				currentToolName = ""
				currentCallID = ""
				jsonAccum = ""
			case "response.completed":
				usage := UsageInfo{}
				if event.Response.Usage.InputTokens != 0 || event.Response.Usage.OutputTokens != 0 {
					usage.InputTokens = int(event.Response.Usage.InputTokens)
					usage.OutputTokens = int(event.Response.Usage.OutputTokens)
					usage.CacheReadTokens = int(event.Response.Usage.InputTokensDetails.CachedTokens)
					usage.InputTokens -= usage.CacheReadTokens
					if usage.InputTokens < 0 {
						usage.InputTokens = 0
					}
				}
				events <- StreamEnd{StopReason: "end_turn", Usage: usage}
			}

			go readNext()
		}

		if err := stream.Err(); err != nil {
			errs <- classifyOpenAIError(err)
		}
	}()

	return events, errs
}

func buildOpenAIInput(messages []conversation.Message) responses.ResponseInputParam {
	var input responses.ResponseInputParam
	for _, m := range messages {
		if m.Role == "assistant" {
			for _, tb := range m.ThinkingBlocks {
				input = append(input, responses.ResponseInputItemParamOfReasoning(
					tb.Signature,
					[]responses.ResponseReasoningItemSummaryParam{{Text: tb.Thinking}},
				))
			}
			if m.Content != "" {
				input = append(input, responses.ResponseInputItemUnionParam{
					OfMessage: &responses.EasyInputMessageParam{
						Role: responses.EasyInputMessageRoleAssistant,
						Content: responses.EasyInputMessageContentUnionParam{
							OfString: param.NewOpt(m.Content),
						},
					},
				})
			}
			for _, tu := range m.ToolUses {
				argsJSON, _ := json.Marshal(tu.Arguments)
				input = append(input, responses.ResponseInputItemUnionParam{
					OfFunctionCall: &responses.ResponseFunctionToolCallParam{
						Name:      tu.ToolName,
						CallID:    tu.ToolUseID,
						Arguments: string(argsJSON),
					},
				})
			}
		} else if len(m.ToolResults) > 0 {
			for _, tr := range m.ToolResults {
				input = append(input, responses.ResponseInputItemUnionParam{
					OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
						CallID: tr.ToolUseID,
						Output: tr.Content,
					},
				})
			}
		} else {
			input = append(input, responses.ResponseInputItemUnionParam{
				OfMessage: &responses.EasyInputMessageParam{
					Role: responses.EasyInputMessageRoleUser,
					Content: responses.EasyInputMessageContentUnionParam{
						OfString: param.NewOpt(m.Content),
					},
				},
			})
		}
	}
	return input
}

func classifyOpenAIError(err error) error {
	switch err.(type) {
	case *LLMError, *AuthenticationError, *RateLimitError, *NetworkError, *ServerError, *ContextTooLongError:
		return err
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		msg := apiErr.Error()
		if status == 413 || (status == 400 && ContainsContextLengthError(msg)) {
			return &ContextTooLongError{Message: fmt.Sprintf("Context Too Long: %s", msg)}
		}
		if status == 401 {
			return &AuthenticationError{Message: fmt.Sprintf("Invalid API key: %s", msg)}
		}
		if status == 429 {
			retryAfter := ""
			if apiErr.Response != nil {
				retryAfter = apiErr.Response.Header.Get("Retry-After")
			}
			return &RateLimitError{Message: "Rate limit error, please wait.", RetryAfter: retryAfter}
		}
		if status >= 500 || status == 408 {
			retryAfter := ""
			if apiErr.Response != nil {
				retryAfter = apiErr.Response.Header.Get("Retry-After")
			}
			return &ServerError{
				Message:    fmt.Sprintf("OpenAI API error (%d): %s", status, msg),
				RetryAfter: retryAfter,
			}
		}
		return &LLMError{Message: fmt.Sprintf("OpenAI API error (%d): %s", status, msg)}
	}
	return &NetworkError{Message: fmt.Sprintf("Network error: %s", err.Error())}
}
