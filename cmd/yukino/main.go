package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/crash_log"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/hooks"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/remote"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/ui"
)

func main() {
	defer tools.RunExitCleanups()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() { <-signals; tools.RunExitCleanups(); os.Exit(1) }()

	recordExit := crash_log.Install()
	defer recordExit()
	defer func() {
		if r := recover(); r != nil {
			crash_log.RecordPanic("main", r, debug.Stack())
			panic(r)
		}
	}()

	// Fill the process environment from the global config's env block before
	// anything reads it.
	config.HydrateConfigEnv()

	remoteAddr := ""
	var filteredArgs []string
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--remote" {
			remoteAddr = ":18888"
			if i+1 < len(os.Args) && os.Args[i+1][0] != '-' {
				remoteAddr = os.Args[i+1]
				i++
			}
		} else {
			filteredArgs = append(filteredArgs, os.Args[i])
		}
	}

	cfg, err := config.LoadConfig("", config.LoadOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		tools.RunExitCleanups()
		os.Exit(1)
	}

	validHooks := cfg.Hooks
	if err := hooks.Validate(validHooks); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: hook configuration is invalid, starting with no hooks:\n%s\n", err)
		validHooks = nil
	}

	if userPrompt, outputFormat, ok := parsePrintFlags(os.Args[1:]); ok {
		if userPrompt == "" {
			buf, _ := os.ReadFile("/dev/stdin")
			userPrompt = string(buf)
		}
		if userPrompt == "" {
			fmt.Fprintf(os.Stderr, "Error: -p requires a prompt argument or stdin input\n")
			tools.RunExitCleanups()
			os.Exit(1)
		}
		if err := runPrint(userPrompt, cfg, validHooks, outputFormat); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			tools.RunExitCleanups()
			os.Exit(1)
		}
		return
	}

	if remoteAddr != "" {
		srv := remote.NewServer(cfg.Providers, cfg.MCPServers, validHooks, remoteAddr, cfg.EnableCoordinatorMode, !cfg.ForkEnabled(), cfg.DefaultProvider)
		srv.MCPLoading = cfg.MCPLoading
		srv.LSPServers = cfg.LspServers
		if err := srv.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Remote server error: %s\n", err)
			tools.RunExitCleanups()
			os.Exit(1)
		}
		return
	}

	m := ui.New(cfg.Providers, cfg.MCPServers, validHooks, cfg.Sandbox, cfg.DefaultProvider)
	m.EnableCoordinatorMode = cfg.EnableCoordinatorMode
	m.MCPLoading = cfg.MCPLoading
	m.LSPServers = cfg.LspServers
	m.ForkDisabled = !cfg.ForkEnabled()
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if disposable, ok := finalModel.(interface{ Dispose() error }); ok {
		_ = disposable.Dispose()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		tools.RunExitCleanups()
		os.Exit(1)
	}
}
