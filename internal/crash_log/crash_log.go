package crash_log

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

func logPath() string {
	return storage.YukinoPath("crash.log")
}

// Record appends a timestamped entry to the crash log. Write failures are
// silently ignored so diagnostics never crash the process.
func Record(text string) {
	if err := storage.MkdirPrivateSync(storage.GetYukinoDir()); err != nil {
		return
	}
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	_ = storage.AppendPrivateFileSync(logPath(), []byte(fmt.Sprintf("[%s] %s\n", stamp, text)))
}

// RecordPanic records an exception with its full stack trace; context
// identifies the originating layer.
func RecordPanic(context string, value any, stack []byte) {
	Record(fmt.Sprintf("crash [%s] %v\n%s", context, value, stack))
}

var exitRecorded atomic.Bool

// RecordExit writes the exit marker; subsequent calls are no-ops. Whichever
// path arrives first wins.
func RecordExit(code any) {
	if !exitRecorded.CompareAndSwap(false, true) {
		return
	}
	Record(fmt.Sprintf("exit pid=%d code=%v", os.Getpid(), code))
}

// Install records crash diagnostics; call once at process startup. A start
// line marks the beginning of a run, an exit line is written on graceful
// shutdown, and panic handlers capture errors that escape to the top of the
// process. Together they determine the exit mode: crash + exit → crashed;
// start + exit only → clean shutdown; start only → killed externally.
func Install() func() {
	Record(fmt.Sprintf("start pid=%d", os.Getpid()))
	if err := storage.MkdirPrivateSync(storage.GetYukinoDir()); err == nil {
		if f, err := storage.OpenPrivateFile(logPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND); err == nil {
			_ = debug.SetCrashOutput(f, debug.CrashOptions{})
			_ = f.Close()
		}
	}
	return func() {
		RecordExit(0)
	}
}
