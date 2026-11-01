package teams

import (
	"os"
	"runtime"
)

func DetectBackend() TeamMode { return detectBackend() }

func detectBackend() TeamMode {
	if runtime.GOOS == "windows" {
		return ModeInProcess
	}
	return detectBackendFromEnv()
}

func detectBackendFromEnv() TeamMode {
	if os.Getenv("TMUX") != "" {
		return ModeTmux
	}
	if os.Getenv("ITERM_SESSION_ID") != "" {
		return ModeITerm
	}
	return ModeInProcess
}
