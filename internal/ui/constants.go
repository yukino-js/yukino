package ui

const (
	AgentsDialogTitle    = "Agents & tasks"
	AgentsDialogSubtitle = "teammates and running background tasks"
	AgentsDialogEmpty    = "No teammates or running background tasks"
)

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
