package subagent

// Agent role names (src/subagent/constants.ts).
const (
	PlanAgentName    = "plan"
	ExploreAgentName = "explore"
	ReviewAgentName  = "review"
)

const AgentToolDescription = "Launch an agent to handle complex, multi-step tasks."

const GeneralPurposeAgentDescription = "General-purpose agent for researching complex questions, exploring codebase, and executing multi-step tasks."

const PlanAgentDescription = "Investigate the existing architecture and propose a concrete implementation plan with relevant files, constraints, and verification steps. Read-only: do not create, edit, or delete files. Return unresolved questions to the parent agent."

const ExploreAgentDescription = "Find code, trace relevant call paths, and report evidence with file paths and line numbers. Use Glob, Grep, ReadFile, and read-only shell commands. Do not modify files; return missing context or blockers to the parent agent."

const ReviewAgentDescription = "Review pull requests for correctness, project conventions, performance, test coverage, and security. Read the diff and relevant code, then return concise findings with file paths, line numbers, impact, and suggested improvements. Do not modify files or publish reviews; return missing context or blockers to the parent."

const DescriptionParameterDescription = "Short description of what the agent will do"

const PromptParameterDescription = "The task for the agent to perform"

const PlanAgentPrompt = "Use the inherited conversation to investigate the user's current task and return a concrete implementation plan with relevant files, constraints, verification steps, and unresolved questions. Do not modify files. Return the complete plan directly; the parent host will save it and request user approval."

const InheritedToolResult = "This tool call is running in the parent agent. Continue with the assigned task using the inherited context."

const SubagentTypeDescription = `Agent role. Defaults to general-purpose, independently of fork. With kind="teammate", plan starts a planning phase and continues implementation after approval; with kind="subagent", plan remains read-only.`

const ForkParameterDescription = "Copy the parent conversation when true; start a fresh conversation when false. This does not restore a previous subagent's private conversation. Independent of subagent_type. Defaults to the session's enable_fork setting (true when unset)."

const AgentKindDescription = "Execution kind. Defaults to teammate: a persistent agent in the selected team, created automatically if none exists. Specify team_name when multiple teams exist. Use an agent team with teammate when communication or follow-up assignments are needed. Use subagent for a one-shot tool result without messaging or resumption."

const AgentModelDescription = "Override the model for this agent. Most tasks do not need this field; omit it to use the configured model."

const PlanAgentUnavailableError = "The built-in plan agent is unavailable."
const ForkContextRequiredError = "Forking requires parent conversation context. Set fork to false to start a fresh conversation."
const EmptyPlanError = "The plan agent returned no plan."
