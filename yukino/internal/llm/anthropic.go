package llm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/conversation"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

const anthropicStreamIdleTimeout = 5 * time.Minute

const nativeToolSearchBeta = "advanced-tool-use-2025-11-20"

func markToolsForCache(sdkTools []anthropic.ToolUnionParam) {
	for i := len(sdkTools) - 1; i >= 0; i-- {
		t := sdkTools[i].OfTool
		if t == nil || t.DeferLoading.Valid() {
			continue
		}
		t.CacheControl = anthropic.NewCacheControlEphemeralParam()
		return
	}
}

func needsToolSearchBeta(toolSchemas []map[string]any) bool {
	for _, s := range toolSchemas {
		if deferLoading, _ := s["defer_loading"].(bool); deferLoading {
			return true
		}
	}
	return false
}

type anthropicClient struct {
	client anthropic.Client
	model  string
	cfg    config.ProviderConfig
	// Effective logical thinking level (clamped to what the provider
	// supports); translated to a token budget or an adaptive effort per
	// request.
	thinkingLevel string
	systemPrompt  string
}

func newAnthropicClient(cfg *config.ProviderConfig, systemPrompt string) (*anthropicClient, error) {
	apiKey := config.ResolveAPIKey(cfg)
	if apiKey == "" {
		return nil, &AuthenticationError{
			Message: AnthropicAPIKeyMissingError,
		}
	}

	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(cfg.BaseURL),
	)

	return &anthropicClient{
		client:        client,
		model:         cfg.Model,
		cfg:           *cfg,
		thinkingLevel: config.GetThinkingLevel(cfg),
		systemPrompt:  systemPrompt,
	}, nil
}

func (c *anthropicClient) SetSystemPrompt(prompt string) {
	c.systemPrompt = prompt
}

func (c *anthropicClient) SetMaxOutputTokens(tokens int) {
	c.cfg.MaxOutputTokens = float64(tokens)
	c.SetThinkingLevel(c.thinkingLevel)
}

func (c *anthropicClient) SetThinkingLevel(level string) string {
	c.thinkingLevel = config.ClampThinkingLevel(&c.cfg, level)
	return c.thinkingLevel
}

func (c *anthropicClient) GetThinkingLevel() string {
	return c.thinkingLevel
}

func (c *anthropicClient) GetSupportedThinkingLevels() []string {
	return config.GetSupportedThinkingLevels(&c.cfg)
}

func (c *anthropicClient) GetOutputBudget() OutputBudget {
	return OutputBudget{
		MaxOutputTokens: config.GetMaxOutputTokens(&c.cfg),
		ThinkingTokens:  config.ThinkingBudgetForProvider(&c.cfg, c.thinkingLevel),
	}
}

func (c *anthropicClient) Stream(ctx context.Context, conv *conversation.Manager, toolSchemas []map[string]any) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent, 64)
	errs := make(chan error, 1)

	msgs := buildAnthropicMessages(conversation.EnsureToolPairing(conv.GetMessages()))

	var sdkTools []anthropic.ToolUnionParam
	sendToolSearchBeta := needsToolSearchBeta(toolSchemas)
	for _, s := range toolSchemas {
		inputSchema, _ := s["input_schema"].(map[string]any)
		props, _ := inputSchema["properties"]
		required, _ := inputSchema["required"].([]string)
		desc, _ := s["description"].(string)
		tool := &anthropic.ToolParam{
			Name:        s["name"].(string),
			Description: param.NewOpt(desc),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: props,
				Required:   required,
			},
		}
		if deferLoading, _ := s["defer_loading"].(bool); deferLoading {
			tool.DeferLoading = param.NewOpt(true)
		}
		sdkTools = append(sdkTools, anthropic.ToolUnionParam{OfTool: tool})
	}

	go func() {
		defer close(events)
		defer close(errs)

		requestConfig := c.cfg
		requestConfig.MaxOutputTokens = float64(maxOutputTokens(ctx, config.GetMaxOutputTokens(&c.cfg)))
		maxTokens := int64(config.GetMaxOutputTokens(&requestConfig))
		params := anthropic.MessageNewParams{
			Model:     c.model,
			MaxTokens: maxTokens,
			System: []anthropic.TextBlockParam{{
				Text:         c.systemPrompt,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			}},
			Messages: msgs,
		}
		markLastUserTailForCache(params.Messages)
		level := config.ClampThinkingLevel(&requestConfig, c.GetThinkingLevel())
		if c.cfg.Reasoning == nil || *c.cfg.Reasoning {
			switch {
			case level == "off":
				params.Thinking = anthropic.ThinkingConfigParamUnion{
					OfDisabled: &anthropic.ThinkingConfigDisabledParam{},
				}
			case c.cfg.ThinkingMode == "adaptive":
				if effort := config.ToAnthropicThinkingEffort(level, &c.cfg); effort != "" {
					params.Thinking = anthropic.ThinkingConfigParamUnion{
						OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
					}
					params.OutputConfig = anthropic.OutputConfigParam{
						Effort: anthropic.OutputConfigEffort(effort),
					}
				}
			default:
				thinkingTokens := config.ThinkingBudgetForProvider(&requestConfig, level)
				if reserve := int(maxTokens) - config.MinThinkingAnswerTokens; thinkingTokens > reserve {
					thinkingTokens = reserve
				}
				if thinkingTokens >= config.MinThinkingBudgetTokens {
					params.Thinking = anthropic.ThinkingConfigParamUnion{
						OfEnabled: &anthropic.ThinkingConfigEnabledParam{
							BudgetTokens: int64(thinkingTokens),
						},
					}
				} else {
					params.Thinking = anthropic.ThinkingConfigParamUnion{
						OfDisabled: &anthropic.ThinkingConfigDisabledParam{},
					}
				}
			}
		}
		if len(sdkTools) > 0 {
			markToolsForCache(sdkTools)
			params.Tools = sdkTools
		}

		var reqOpts []option.RequestOption
		if sendToolSearchBeta {
			reqOpts = append(reqOpts, option.WithHeaderAdd("anthropic-beta", nativeToolSearchBeta))
		}

		stream := c.client.Messages.NewStreaming(ctx, params, reqOpts...)
		defer stream.Close()

		var currentToolName, currentToolID, jsonAccum string
		var thinkingAccum, thinkingSignature string
		inThinking := false
		var accMessage anthropic.Message
		// Terminal-event guard: a stream cut between message_start and
		// message_stop (gateway dropping the SSE) must not be committed as a
		// complete end_turn turn with near-zero usage.
		sawMessageStop := false

		type sseResult struct {
			hasNext bool
		}
		nextCh := make(chan sseResult, 1)

		readNext := func() {
			nextCh <- sseResult{hasNext: stream.Next()}
		}

		idle := time.NewTimer(anthropicStreamIdleTimeout)
		defer idle.Stop()

		go readNext()
		for {
			var res sseResult
			select {
			case <-ctx.Done():
				errs <- &NetworkError{Message: fmt.Sprintf("context cancelled: %v", ctx.Err())}
				return
			case <-idle.C:
				errs <- &NetworkError{Message: fmt.Sprintf("stream idle timeout: no SSE events for %s", anthropicStreamIdleTimeout)}
				return
			case res = <-nextCh:
			}

			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(anthropicStreamIdleTimeout)

			if !res.hasNext {
				break
			}

			event := stream.Current()
			accMessage.Accumulate(event)
			if mde, ok := event.AsAny().(anthropic.MessageDeltaEvent); ok {
				if mde.Usage.InputTokens > 0 {
					accMessage.Usage.InputTokens = mde.Usage.InputTokens
				}
				if mde.Usage.CacheReadInputTokens > 0 {
					accMessage.Usage.CacheReadInputTokens = mde.Usage.CacheReadInputTokens
				}
				if mde.Usage.CacheCreationInputTokens > 0 {
					accMessage.Usage.CacheCreationInputTokens = mde.Usage.CacheCreationInputTokens
				}
			}
			switch ev := event.AsAny().(type) {
			case anthropic.MessageStopEvent:
				sawMessageStop = true
			case anthropic.ContentBlockStartEvent:
				switch ev.ContentBlock.Type {
				case "redacted_thinking":
					events <- RedactedThinkingComplete{Data: ev.ContentBlock.Data}
				case "thinking":
					inThinking = true
					thinkingAccum = ""
					thinkingSignature = ""
				case "tool_use":
					currentToolName = ev.ContentBlock.Name
					currentToolID = ev.ContentBlock.ID
					jsonAccum = ""
					events <- ToolCallStart{ToolName: currentToolName, ToolID: currentToolID}
				}
			case anthropic.ContentBlockDeltaEvent:
				switch delta := ev.Delta.AsAny().(type) {
				case anthropic.ThinkingDelta:
					thinkingAccum += delta.Thinking
					events <- ThinkingDelta{Text: delta.Thinking}
				case anthropic.SignatureDelta:
					thinkingSignature = delta.Signature
				case anthropic.TextDelta:
					events <- TextDelta{Text: delta.Text}
				case anthropic.InputJSONDelta:
					jsonAccum += delta.PartialJSON
					events <- ToolCallDelta{Text: delta.PartialJSON}
				}
			case anthropic.ContentBlockStopEvent:
				if inThinking {
					events <- ThinkingComplete{
						Thinking:  thinkingAccum,
						Signature: thinkingSignature,
					}
					inThinking = false
				}
				if currentToolName != "" {
					args, parseError := ParseToolArguments(jsonAccum)
					events <- ToolCallComplete{
						ToolID:     currentToolID,
						ToolName:   currentToolName,
						Arguments:  args,
						ParseError: parseError,
					}
					currentToolName = ""
					currentToolID = ""
					jsonAccum = ""
				}
			}

			go readNext()
		}

		if err := stream.Err(); err != nil {
			errs <- classifyAnthropicError(err)
			return
		}

		// Without message_stop the response is truncated, not complete: throw a
		// NetworkError instead of yielding stream_end so the run surfaces an
		// error rather than persisting a half response as a finished turn.
		if !sawMessageStop {
			errs <- &NetworkError{Message: "Anthropic stream ended without message_stop; response was truncated"}
			return
		}

		stopReason := string(accMessage.StopReason)
		if stopReason == "" {
			stopReason = "end_turn"
		}
		usage := UsageInfo{
			InputTokens:         int(accMessage.Usage.InputTokens),
			OutputTokens:        int(accMessage.Usage.OutputTokens),
			CacheReadTokens:     int(accMessage.Usage.CacheReadInputTokens),
			CacheCreationTokens: int(accMessage.Usage.CacheCreationInputTokens),
		}
		events <- StreamEnd{StopReason: stopReason, Usage: usage}
	}()

	return events, errs
}

func markLastUserTailForCache(messages []anthropic.MessageParam) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != anthropic.MessageParamRoleUser {
			continue
		}
		blocks := messages[i].Content
		if len(blocks) == 0 {
			return
		}
		// Prefer the last non-image block: some gateways reject cache_control
		// on image blocks. Fall back to the true tail if everything is an
		// image.
		idx := len(blocks) - 1
		for j := len(blocks) - 1; j >= 0; j-- {
			if blocks[j].OfImage == nil {
				idx = j
				break
			}
		}
		last := &blocks[idx]
		switch {
		case last.OfText != nil:
			last.OfText.CacheControl = anthropic.NewCacheControlEphemeralParam()
		case last.OfToolResult != nil:
			last.OfToolResult.CacheControl = anthropic.NewCacheControlEphemeralParam()
		case last.OfDocument != nil:
			last.OfDocument.CacheControl = anthropic.NewCacheControlEphemeralParam()
		case last.OfImage != nil:
			last.OfImage.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		return
	}
}

func buildAnthropicMessages(messages []conversation.Message) []anthropic.MessageParam {
	var result []anthropic.MessageParam
	for _, m := range messages {
		if m.Role == "assistant" {
			var blocks []anthropic.ContentBlockParamUnion
			for _, tb := range m.ThinkingBlocks {
				if tb.Type == "redacted_thinking" {
					blocks = append(blocks, anthropic.NewRedactedThinkingBlock(tb.Data))
					continue
				}
				blocks = append(blocks, anthropic.NewThinkingBlock(tb.Signature, tb.Thinking))
			}
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tu := range m.ToolUses {
				name := tu.ToolName
				if name == "ComputerUse" {
					name = "computer"
				}
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    tu.ToolUseID,
						Name:  name,
						Input: tu.Arguments,
					},
				})
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropic.NewTextBlock(""))
			}
			result = append(result, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleAssistant,
				Content: blocks,
			})
		} else if len(m.ToolResults) > 0 {
			var blocks []anthropic.ContentBlockParamUnion
			for _, tr := range m.ToolResults {
				content := []anthropic.ToolResultBlockParamContentUnion{{
					OfText: &anthropic.TextBlockParam{Text: tr.Content},
				}}
				if structured := toolResultContentBlocks(tr.ContentBlocks); len(structured) > 0 {
					content = structured
				}
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolResult: &anthropic.ToolResultBlockParam{
						ToolUseID: tr.ToolUseID,
						IsError:   param.NewOpt(tr.IsError),
						Content:   content,
					},
				})
			}
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			result = append(result, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: blocks,
			})
		} else {
			// Anthropic messages must alternate roles. Merge every consecutive
			// user turn, including reminders or steering immediately after
			// tool results.
			if n := len(result); n > 0 && result[n-1].Role == anthropic.MessageParamRoleUser {
				result[n-1].Content = append(result[n-1].Content, anthropic.NewTextBlock(m.Content))
				continue
			}
			result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	return result
}

func classifyAnthropicError(err error) error {
	switch err.(type) {
	case *LLMError, *AuthenticationError, *RateLimitError, *NetworkError, *ServerError, *ContextTooLongError:
		return err
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		msg := apiErr.Error()
		// 413 Request Too Large — the request body itself exceeds size limits.
		// Prompt-too-long (token count over the context window) arrives as 400
		// invalid_request_error, not 413.
		if status == 413 || (status == 400 && ContainsContextLengthError(msg)) {
			return &ContextTooLongError{Message: fmt.Sprintf("Prompt too long: %s", msg)}
		}
		if status == 401 {
			return &AuthenticationError{Message: fmt.Sprintf("Invalid API key: %s", msg)}
		}
		if status == 429 {
			retryAfter := ""
			if apiErr.Response != nil {
				retryAfter = apiErr.Response.Header.Get("Retry-After")
			}
			message := "Rate Limited"
			if retryAfter != "" {
				if _, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err != nil {
					message += ", please wait."
				} else {
					message += fmt.Sprintf(", retry after %ss.", strings.TrimSpace(retryAfter))
				}
			} else {
				message += ", please wait."
			}
			return &RateLimitError{Message: message, RetryAfter: retryAfter}
		}
		if status >= 500 || status == 408 {
			retryAfter := ""
			if apiErr.Response != nil {
				retryAfter = apiErr.Response.Header.Get("Retry-After")
			}
			return &ServerError{
				Message:    fmt.Sprintf("Anthropic API error (%d): %s", status, msg),
				RetryAfter: retryAfter,
			}
		}
		return &LLMError{Message: fmt.Sprintf("Anthropic API error (%d): %s", status, msg)}
	}
	return &NetworkError{Message: fmt.Sprintf("Network error: %s", err.Error())}
}

func toolResultContentBlocks(raw []map[string]any) []anthropic.ToolResultBlockParamContentUnion {
	if len(raw) == 0 {
		return nil
	}
	out := make([]anthropic.ToolResultBlockParamContentUnion, 0, len(raw))
	for _, b := range raw {
		blockType, _ := b["type"].(string)
		if blockType != "tool_reference" {
			return nil
		}
		name, _ := b["tool_name"].(string)
		if name == "" {
			return nil
		}
		out = append(out, anthropic.ToolResultBlockParamContentUnion{
			OfToolReference: &anthropic.ToolReferenceBlockParam{ToolName: name},
		})
	}
	return out
}
