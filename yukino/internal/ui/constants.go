package ui

import "github.com/yukino-js/yukino/yukino/internal/commands"

const (
	AgentsDialogTitle    = "Agents & tasks"
	AgentsDialogSubtitle = "teammates and running background tasks"
	AgentsDialogEmpty    = "No teammates or running background tasks"
)

var CommandTags = []string{"[skill]", commands.CustomCommandTag}

// At-mention cache lifetime: covers edits made outside the app (user editor,
// git) that produce no fileFactsVersion bump.
const FileCacheTTLMs = 30_000

// MaxRecentTools is the maximum number of recent tool names (deduplicated)
// passed to the memory recall selector.
const MaxRecentTools = 10

var Protocols = []string{"anthropic", "openai", "openai-compat"}

// Other is the focus sentinel for the free-text row; never a real model id.
const Other = "\u0000other"
const OtherLabel = "Other (type your own)"

const TabSize = 2

// CellPadding is one space of padding on each side of a cell, in terminal
// columns.
const CellPadding = 2

var StatusMessages = map[string]string{
	"loading": "Fetching models…",
	"empty":   "No models returned by this provider",
	"error":   "Model discovery failed for this provider",
}

const ClientNotReady = "Client not ready."

const TerminalTooShort = "Terminal too short"

const UserBashProgressExcluded = "User command · excluded in next model context"
const UserBashProgressIncluded = "User command · included in next model context"

var ThinkingDescriptions = map[string]string{
	"off":     "Reasoning off",
	"minimal": "Brief reasoning",
	"low":     "Light reasoning",
	"medium":  "Balanced reasoning",
	"high":    "Deep reasoning",
	"xhigh":   "Extra-deep reasoning",
	"max":     "Most thorough reasoning",
}

// Icons mirrors the ICONS map in src/ui/constants.ts.
var Icons = struct {
	Active     string
	Arrow      string
	Branch     string
	Collapsed  string
	Expanded   string
	Inactive   string
	LastBranch string
	Prompt     string
	Success    string
	Error      string
	Tree       string
}{
	Active:     "◉",
	Arrow:      "→",
	Branch:     "├─",
	Collapsed:  "⊞",
	Expanded:   "⊟",
	Inactive:   "○",
	LastBranch: "└─",
	Prompt:     ">",
	Success:    "✓",
	Error:      "✗",
	Tree:       "│",
}

var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var PermissionModeCycle = []string{"default", "acceptEdits", "bypassPermissions"}

var ModeDisplay = map[string]string{
	"default":           "Default",
	"acceptEdits":       "Accept Edits",
	"plan":              "Plan",
	"bypassPermissions": "YOLO",
}

type PermissionOption struct {
	Label  string
	Action string
}

var PermissionOptions = []PermissionOption{
	{Label: "Yes", Action: "allow"},
	{Label: "Yes, remember the scopes above for all agents in this project", Action: "allowAlways"},
	{Label: "No", Action: "deny"},
}

var PlanApprovalOptions = []string{
	"Yes, enter YOLO mode (auto-approve all)",
	"Yes, manually approve edits",
	"Tell Yukino what to change",
}

var RestoreOptions = []string{
	"Restore code and conversation",
	"Restore conversation only",
	"Restore code only",
	"Never mind",
}
