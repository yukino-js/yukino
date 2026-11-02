package agent

import "github.com/yukino-js/yukino/yukino/internal/goal"

// NoPersistentGoalReminder mirrors agent/prompts.ts NO_PERSISTENT_GOAL_REMINDER.
// It replaces a stale goal reminder once the goal is cleared.
const NoPersistentGoalReminder = goal.PersistentGoalMarker +
	"\nNo persistent goal is set. Follow the current user request.\n</persistent-goal>"

// GoalBudgetLimitedNotice mirrors agent/prompts.ts GOAL_BUDGET_LIMITED_NOTICE.
// It is stamped onto tool calls when the goal budget is exhausted so the model
// reports progress instead of executing tools.
const GoalBudgetLimitedNotice = "Goal token budget reached; no further tools will be executed. Report existing progress."
