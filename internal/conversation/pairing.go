package conversation

import "strings"

const (
	InterruptedToolResult      = "Tool execution was interrupted. The tool may or may not have completed; verify before relying on its effects."
	RejectedToolResult         = "The user rejected this tool use. Nothing was changed (for file edits, the new content was NOT written)."
	rejectedWithFeedbackPrefix = " To tell you how to proceed, the user said:\n"
)

func RejectedToolResultWithFeedback(feedback string) string {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return RejectedToolResult
	}
	return RejectedToolResult + rejectedWithFeedbackPrefix + feedback
}

func EnsureToolPairing(messages []Message) []Message {
	resolved := make(map[string]struct{})
	issued := make(map[string]struct{})
	for _, m := range messages {
		for _, tr := range m.ToolResults {
			resolved[tr.ToolUseID] = struct{}{}
		}
		for _, tu := range m.ToolUses {
			issued[tu.ToolUseID] = struct{}{}
		}
	}

	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		if len(m.ToolResults) > 0 {
			kept := make([]ToolResultBlock, 0, len(m.ToolResults))
			for _, tr := range m.ToolResults {
				if _, ok := issued[tr.ToolUseID]; ok {
					kept = append(kept, tr)
				}
			}
			if len(kept) == 0 && m.Content == "" && len(m.ToolUses) == 0 {
				continue
			}
			m.ToolResults = kept
		}

		out = append(out, m)

		var missing []ToolResultBlock
		for _, tu := range m.ToolUses {
			if _, ok := resolved[tu.ToolUseID]; ok {
				continue
			}
			missing = append(missing, ToolResultBlock{
				ToolUseID: tu.ToolUseID,
				Content:   InterruptedToolResult,
				IsError:   true,
			})
			resolved[tu.ToolUseID] = struct{}{}
		}
		if len(missing) > 0 {
			out = append(out, Message{Role: "user", ToolResults: missing})
		}
	}
	return out
}
