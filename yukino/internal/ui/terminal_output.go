package ui

import (
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
)

// isSyncOutputSupported detects DEC 2026 synchronized-output support from the
// environment instead of a DECRQM query, which several terminals do not
// implement. Unrecognized terminals count as unsupported: emitting the
// sequences at a terminal that mishandles them can leave garbage on screen.
func isSyncOutputSupported() bool {
	if os.Getenv("TMUX") != "" {
		return false
	}
	termValue := os.Getenv("TERM")
	return syncOutputTermPrograms[os.Getenv("TERM_PROGRAM")] ||
		strings.Contains(termValue, "kitty") ||
		termValue == "xterm-ghostty" ||
		strings.HasPrefix(termValue, "foot") ||
		strings.Contains(termValue, "alacritty") ||
		os.Getenv("KITTY_WINDOW_ID") != "" ||
		os.Getenv("ZED_TERM") != "" ||
		os.Getenv("WT_SESSION") != "" ||
		vteVersion() >= 6800
}

var leadingIntPattern = regexp.MustCompile(`^[+-]?[0-9]+`)

// vteVersion mirrors Number.parseInt(env.VTE_VERSION ?? "", 10); a value with
// no leading digits yields math.MinInt so the >= comparison fails like NaN.
func vteVersion() int {
	match := leadingIntPattern.FindString(strings.TrimSpace(os.Getenv("VTE_VERSION")))
	if match == "" {
		return math.MinInt
	}
	value, err := strconv.Atoi(match)
	if err != nil {
		if strings.HasPrefix(match, "-") {
			return math.MinInt
		}
		return math.MaxInt
	}
	return value
}

// syncOutputWriter frames every write in BSU/ESU. bubbletea's standard
// renderer flushes each frame as a single write, so one write is one frame —
// the granularity the reference achieves by corking stdout around each Ink
// render pass. It must keep implementing term.File (Fd/Read/Close) so
// bubbletea still recognizes the output TTY and delivers WindowSizeMsg.
type syncOutputWriter struct{ out *os.File }

var _ term.File = syncOutputWriter{}

func (w syncOutputWriter) Fd() uintptr { return w.out.Fd() }

func (w syncOutputWriter) Read(p []byte) (int, error) { return w.out.Read(p) }

func (w syncOutputWriter) Close() error { return w.out.Close() }

func (w syncOutputWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return w.out.Write(p)
	}
	buf := make([]byte, 0, len(p)+len(bsu)+len(esu))
	buf = append(buf, bsu...)
	buf = append(buf, p...)
	buf = append(buf, esu...)
	if _, err := w.out.Write(buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

// InstallTerminalOutput enables synchronized output on out when it is a
// terminal that supports it. The reference's Ink-specific frame interception
// (static replay, resize buffering, soft-wrap cursor grouping) is replaced by
// bubbletea's renderer, which already diffs and batches frames.
func InstallTerminalOutput(out io.Writer) io.Writer {
	file, ok := out.(*os.File)
	if !ok || !term.IsTerminal(file.Fd()) || !isSyncOutputSupported() {
		return out
	}
	return syncOutputWriter{out: file}
}
