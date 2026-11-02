package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yukino-js/yukino/yukino/internal/a2a"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/crash_log"
	"github.com/yukino-js/yukino/yukino/internal/hooks"
	"github.com/yukino-js/yukino/yukino/internal/logger"
	"github.com/yukino-js/yukino/yukino/internal/remote"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/telemetry"
	"github.com/yukino-js/yukino/yukino/internal/tools"
	"github.com/yukino-js/yukino/yukino/internal/ui"
)

// loadRuntimeConfig loads the app config with the project .mcp.json servers
// merged and validates the hook configuration, mirroring the reference mode
// runners.
func loadRuntimeConfig(allowEmptyProviders bool) (*config.AppConfig, []config.HookConfig, error) {
	cfg, err := config.LoadConfig("", config.LoadOptions{AllowEmptyProviders: allowEmptyProviders})
	if err != nil {
		return nil, nil, err
	}
	cwd, err := os.Getwd()
	if err == nil {
		cfg = config.WithProjectMcpServers(cfg, cwd)
	}
	validHooks := cfg.Hooks
	if err := hooks.Validate(validHooks); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: hook configuration is invalid, starting with no hooks:\n%s\n", err)
		validHooks = nil
	}
	return cfg, validHooks, nil
}

// exitNow runs the exit sweeps the reference performs from its process exit
// handler and terminates with the given code.
func exitNow(code int) {
	tools.RunExitCleanups()
	logger.CloseLogger()
	crash_log.RecordExit(code)
	os.Exit(code)
}

func main() {
	defer logger.CloseLogger()
	defer tools.RunExitCleanups()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		sig := <-signals
		if sig == syscall.SIGTERM && telemetry.RemoteSignalHandlersInstalled() {
			// Remote mode owns SIGINT/SIGTERM shutdown.
			return
		}
		tools.RunExitCleanups()
		logger.CloseLogger()
		os.Exit(1)
	}()

	recordExit := crash_log.Install()
	defer recordExit()
	defer func() {
		if r := recover(); r != nil {
			crash_log.RecordPanic("main", r, debug.Stack())
			logger.Global().Fatal(map[string]any{"err": r}, "uncaught exception")
			telemetry.CaptureTelemetryError(fmt.Errorf("%v", r), "uncaught exception")
			// Sync sweep before the telemetry flush: the process is going down,
			// and detached children must not survive it as orphans.
			tools.RunExitCleanups()
			telemetry.ShutdownTelemetry()
			panic(r)
		}
	}()

	// Maintenance mode: `yukino update` runs without loading the runtime or
	// configuration, mirroring the reference CLI dispatch.
	if len(os.Args) > 1 && os.Args[1] == "update" {
		code := runUpdateMode(os.Args[2:])
		exitNow(code)
	}

	// Fill the process environment from the global config's env block before
	// anything reads it: telemetry initialization reads OTEL_*/SENTRY_* from
	// the environment before the config is loaded.
	config.HydrateConfigEnv()

	remoteAddr := ""
	a2aMode := false
	a2aAddr := ""
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--remote":
			remoteAddr = ":18888"
			if i+1 < len(os.Args) && os.Args[i+1][0] != '-' {
				remoteAddr = os.Args[i+1]
				i++
			}
		case "--a2a":
			a2aMode = true
			if i+1 < len(os.Args) && os.Args[i+1][0] != '-' {
				a2aAddr = os.Args[i+1]
				i++
			}
		}
	}

	userPrompt, outputFormat, isPrint := parsePrintFlags(os.Args[1:])

	telemetry.InitializeTelemetry()

	if a2aMode {
		telemetry.SetTelemetryMode(telemetry.ModeA2A)
		if err := a2a.RunA2aServer(a2aAddr); err != nil {
			telemetry.CaptureTelemetryError(err, "main")
			crash_log.RecordError("main", err)
			logger.Global().Fatal(map[string]any{"err": err}, "unhandled runtime error")
			telemetry.ShutdownTelemetry()
			exitNow(-1)
		}
		telemetry.ShutdownTelemetry()
		return
	}

	if isPrint {
		telemetry.SetTelemetryMode(telemetry.ModePrint)
		if userPrompt == "" {
			buf, _ := os.ReadFile("/dev/stdin")
			userPrompt = string(buf)
		}
		if userPrompt == "" {
			fmt.Fprintf(os.Stderr, "Error: -p requires a prompt argument or stdin input\n")
			telemetry.ShutdownTelemetry()
			exitNow(1)
		}
		cfg, validHooks, err := loadRuntimeConfig(false)
		if err != nil {
			telemetry.CaptureTelemetryError(err, "print")
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			telemetry.ShutdownTelemetry()
			exitNow(1)
		}
		if err := runPrint(userPrompt, cfg, validHooks, outputFormat); err != nil {
			telemetry.CaptureTelemetryError(err, "print")
			if !errors.Is(err, errGoalCommand) {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			}
			telemetry.ShutdownTelemetry()
			exitNow(1)
		}
		telemetry.ShutdownTelemetry()
		return
	}

	if remoteAddr != "" {
		cfg, validHooks, err := loadRuntimeConfig(true)
		if err != nil {
			telemetry.CaptureTelemetryError(err, "config")
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			telemetry.ShutdownTelemetry()
			exitNow(1)
		}
		telemetry.SetTelemetryMode(telemetry.ModeRemote)
		if err := logger.InitLogger(logger.InitLoggerOptions{SessionID: session.NewID(), Mode: logger.ModeRemote, Stdout: true}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			telemetry.ShutdownTelemetry()
			exitNow(1)
		}
		srv := remote.NewServer(cfg.Providers, cfg.MCPServers, validHooks, remoteAddr, cfg.EnableCoordinatorMode, cfg.DefaultProvider, cfg.Sandbox, cfg.MemoryEnabled())
		srv.MCPLoading = cfg.MCPLoading
		// Graceful shutdown on Ctrl+C/SIGTERM: stop the server (closes WS/HTTP,
		// disconnects MCP children), sweep detached children and persist the
		// real exit code before telemetry flushes.
		telemetry.InstallRemoteTelemetrySignalHandlers(func(exitCode int) {
			srv.Stop()
			tools.RunExitCleanups()
			crash_log.RecordExit(exitCode)
		})
		if err := srv.Run(); err != nil {
			telemetry.CaptureTelemetryError(err, "remote")
			fmt.Fprintf(os.Stderr, "Remote server error: %s\n", err)
			telemetry.ShutdownTelemetry()
			exitNow(1)
		}
		// Run only returns nil after Stop, which the signal handler drives;
		// the handler flushes telemetry and exits with the signal's code.
		select {}
	}

	cfg, validHooks, err := loadRuntimeConfig(true)
	if err != nil {
		telemetry.CaptureTelemetryError(err, "config")
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		telemetry.ShutdownTelemetry()
		exitNow(1)
	}
	telemetry.SetTelemetryMode(telemetry.ModeTerminal)
	if err := logger.InitLogger(logger.InitLoggerOptions{SessionID: session.NewID(), Mode: logger.ModeTerminal}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		telemetry.ShutdownTelemetry()
		exitNow(1)
	}
	m := ui.New(cfg.Providers, cfg.MCPServers, validHooks, cfg.Sandbox, cfg.DefaultProvider, cfg.PermissionMode, cfg.MemoryEnabled())
	m.EnableCoordinatorMode = cfg.EnableCoordinatorMode
	m.MCPLoading = cfg.MCPLoading
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if disposable, ok := finalModel.(interface{ Dispose() error }); ok {
		_ = disposable.Dispose()
	}
	if err != nil {
		telemetry.CaptureTelemetryError(err, "main")
		crash_log.RecordError("main", err)
		logger.Global().Fatal(map[string]any{"err": err}, "unhandled runtime error")
		telemetry.ShutdownTelemetry()
		exitNow(-1)
	}
	telemetry.ShutdownTelemetry()
}
