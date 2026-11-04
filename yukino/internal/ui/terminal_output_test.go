package ui

import (
	"math"
	"os"
	"testing"
)

func TestSyncOutputDetection(t *testing.T) {
	keys := []string{"TMUX", "TERM", "TERM_PROGRAM", "VTE_VERSION", "KITTY_WINDOW_ID", "ZED_TERM", "WT_SESSION"}
	for _, tc := range []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{"TERM_PROGRAM": "ghostty"}, true},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, true},
		{map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "/tmp/tmux"}, false},
		{map[string]string{"TERM": "xterm-kitty"}, true},
		{map[string]string{"TERM": "xterm-ghostty"}, true},
		{map[string]string{"TERM": "foot-extra"}, true},
		{map[string]string{"TERM": "alacritty"}, true},
		{map[string]string{"KITTY_WINDOW_ID": "1"}, true},
		{map[string]string{"ZED_TERM": "1"}, true},
		{map[string]string{"WT_SESSION": "guid"}, true},
		{map[string]string{"VTE_VERSION": "6800"}, true},
		{map[string]string{"VTE_VERSION": "6799"}, false},
		{map[string]string{"VTE_VERSION": "abc"}, false},
		{map[string]string{"TERM": "xterm-256color"}, false},
		{map[string]string{}, false},
	} {
		for _, key := range keys {
			t.Setenv(key, "")
		}
		for key, value := range tc.env {
			t.Setenv(key, value)
		}
		if got := isSyncOutputSupported(); got != tc.want {
			t.Fatalf("%v => %v; want %v", tc.env, got, tc.want)
		}
	}
}

func TestVteVersion(t *testing.T) {
	for in, want := range map[string]int{"": math.MinInt, "abc": math.MinInt, "6800": 6800, " 6800 ": 6800, "68abc": 68, "-5": -5} {
		t.Setenv("VTE_VERSION", in)
		if got := vteVersion(); got != want {
			t.Fatalf("%q => %d; want %d", in, got, want)
		}
	}
}

func TestSyncOutputWriter(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	sw := syncOutputWriter{out: w}
	n, err := sw.Write([]byte("frame"))
	if err != nil || n != 5 {
		t.Fatal(n, err)
	}
	buf := make([]byte, len("\x1b[?2026hframe\x1b[?2026l"))
	if _, err := r.Read(buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "\x1b[?2026hframe\x1b[?2026l" {
		t.Fatalf("%q", buf)
	}
	n, err = sw.Write(nil)
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
