package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/lsp"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/mcp"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/prompt"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/session"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/skills"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/teams"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/worktree"
)

type teammateArgs struct {
	teamName   string
	memberName string
}

func parseTeammateFlags(args []string) (teammateArgs, bool) {
	var out teammateArgs
	if len(args) == 0 || args[0] != "--teammate" {
		return out, false
	}
	i := 1
	for i < len(args) {
		switch args[i] {
		case "--team-name":
			if i+1 < len(args) {
				out.teamName = args[i+1]
				i += 2
				continue
			}
		case "--agent-name":
			if i+1 < len(args) {
				out.memberName = args[i+1]
				i += 2
				continue
			}
		}
		i++
	}
	return out, true
}

func runTeammate(args teammateArgs) error {
	if args.teamName == "" || args.memberName == "" {
		return fmt.Errorf("--teammate requires --team-name and --agent-name")
	}

	cfg, err := config.LoadConfig("", config.LoadOptions{})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("no providers configured")
	}
	provider := *config.ResolveDefaultProvider(cfg.Providers, cfg.DefaultProvider)

	wd, _ := os.Getwd()
	sessionID := session.NewID()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	skillCatalog := skills.LoadCatalog(wd)
	env := prompt.DetectEnvironment(wd)
	env.Model = provider.Model
	systemPrompt := prompt.BuildSystemPrompt(env, prompt.BuildOptions{})

	client, err := llm.NewClient(&provider, systemPrompt)
	if err != nil {
		return fmt.Errorf("create LLM client: %w", err)
	}

	teamMgr := teams.NewTeamManager(wd)
	team := teamMgr.GetTeam(args.teamName)
	if team == nil {
		team = teams.NewTeam(wd, args.teamName, teams.ModeInProcess)
		teamMgr.CreateTeamWith(team)
	}

	registry := buildTeammateRegistry(ctx, teammateToolOptions{
		WorkDir:    wd,
		Protocol:   provider.Protocol,
		SessionID:  sessionID,
		TeamMgr:    teamMgr,
		TeamName:   args.teamName,
		MemberName: args.memberName,
		MCPServers: cfg.MCPServers,
		LSPServers: cfg.LspServers,
		MCPLoading: cfg.MCPLoading,

		BaseURL:       provider.BaseURL,
		ContextWindow: config.GetContextWindow(&provider),
	})

	defer registry.Dispose()

	member := team.AddMember(args.memberName, client, registry, provider.Protocol)
	member.AgentRef.SkillSection = buildPrintSkillSection(skillCatalog)

	registry.Register(&skills.LoadSkillTool{Catalog: skillCatalog, Host: member.AgentRef})
	registry.Register(&skills.InstallSkillTool{Catalog: skillCatalog})

	addendum := teams.BuildTeammateAddendum(args.teamName, args.memberName, nil)

	fmt.Fprintf(os.Stderr, "[teammate %s/%s] booted, awaiting tasks\n", args.teamName, args.memberName)
	return teams.RunInProcessTeammate(ctx, team, member, "", addendum, streamEventsToStderr())
}

type teammateToolOptions struct {
	WorkDir       string
	Protocol      string
	SessionID     string
	TeamMgr       *teams.TeamManager
	TeamName      string
	MemberName    string
	MCPServers    []config.MCPServerConfig
	LSPServers    []lsp.ServerConfig
	BaseURL       string
	ContextWindow int
	MCPLoading    string
}

func buildTeammateRegistry(ctx context.Context, opts teammateToolOptions) *tools.Registry {
	registry := tools.CreateDefaultToolsWithWorkDir(opts.WorkDir).Registry
	if server, ok := registry.Get("LSP").(*tools.LSPTool); ok {
		server.Configs = opts.LSPServers
	}

	registry.Register(&tools.ToolSearchTool{Registry: registry})
	registry.Register(&tools.McpCallTool{Registry: registry})
	registry.Register(&tools.SyntheticOutputTool{})

	gitRoot := worktree.FindCanonicalGitRoot(opts.WorkDir)
	registry.Register(&tools.EnterWorktreeTool{SessionID: opts.SessionID, RepoRoot: gitRoot})
	registry.Register(&tools.ExitWorktreeTool{RepoRoot: gitRoot})

	registry.Register(&teams.SendMessageTool{TeamMgr: opts.TeamMgr, SenderName: opts.MemberName})
	registry.Register(&teams.TeamTodoWriteTool{TeamMgr: opts.TeamMgr, TeamName: opts.TeamName, AgentName: opts.MemberName})

	if len(opts.MCPServers) > 0 {
		mgr := mcp.NewManager()
		serverConfigs := make([]mcp.ServerConfig, 0, len(opts.MCPServers))
		for _, c := range opts.MCPServers {
			serverConfigs = append(serverConfigs, mcp.ServerConfig{
				Name:      c.Name,
				Command:   c.Command,
				Args:      c.Args,
				URL:       c.URL,
				Transport: c.Transport,
				Headers:   c.Headers,
				Env:       c.Env,
			})
		}
		mgr.LoadConfigs(serverConfigs)
		mgr.RegisterAllTools(ctx, registry)
		mcp.DecideAndApply(registry, opts.BaseURL, opts.Protocol, opts.ContextWindow, opts.MCPLoading)
	}

	return registry
}

func streamEventsToStderr() chan<- agent.AgentEvent {
	ch := make(chan agent.AgentEvent, 32)
	go func() {
		for ev := range ch {
			switch e := ev.(type) {
			case agent.StreamText:
				fmt.Fprint(os.Stderr, e.Text)
			case agent.ToolUseEvent:
				fmt.Fprintf(os.Stderr, "\n[tool %s]\n", e.ToolName)
			case agent.ToolResultEvent:
				summary := strings.TrimSpace(e.Output)
				if len(summary) > 200 {
					summary = summary[:200] + "..."
				}
				fmt.Fprintf(os.Stderr, "[result] %s\n", summary)
			case agent.ErrorEvent:
				fmt.Fprintf(os.Stderr, "[error] %s\n", e.Message)
			case agent.LoopComplete:
				fmt.Fprintf(os.Stderr, "[turn done after %d steps]\n", e.TotalTurns)
			}
		}
	}()
	return ch
}

var _ = conversation.NewManager
