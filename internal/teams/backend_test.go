package teams

import (
	"runtime"
	"testing"
)

func TestDetectBackendFromEnv(t *testing.T) {
	t.Run("inside tmux picks tmux", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/sock,1,0")
		t.Setenv("ITERM_SESSION_ID", "")
		if got := detectBackendFromEnv(); got != ModeTmux {
			t.Errorf("got %q, want tmux", got)
		}
	})
	t.Run("inside iterm2 picks iterm", func(t *testing.T) {
		t.Setenv("TMUX", "")
		t.Setenv("ITERM_SESSION_ID", "w0t0p0:ABC")
		if got := detectBackendFromEnv(); got != ModeITerm {
			t.Errorf("got %q, want iterm", got)
		}
	})
	t.Run("tmux wins over iterm", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/sock,1,0")
		t.Setenv("ITERM_SESSION_ID", "w0t0p0:ABC")
		if got := detectBackendFromEnv(); got != ModeTmux {
			t.Errorf("got %q, want tmux", got)
		}
	})
	t.Run("plain terminal falls back to in-process", func(t *testing.T) {
		t.Setenv("TMUX", "")
		t.Setenv("ITERM_SESSION_ID", "")
		if got := detectBackendFromEnv(); got != ModeInProcess {
			t.Errorf("got %q, want in-process", got)
		}
	})
}

func TestDetectBackendWindowsGuard(t *testing.T) {
	t.Setenv("TMUX", "/tmp/sock,1,0")
	got := detectBackend()
	if runtime.GOOS == "windows" {
		if got != ModeInProcess {
			t.Errorf("windows must stay in-process even inside tmux, got %q", got)
		}
	} else {
		if got != ModeTmux {
			t.Errorf("non-windows inside tmux should pick tmux, got %q", got)
		}
	}
}
