import argparse
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time


def check_terminal(binary, mode, report, accent):
    with tempfile.TemporaryDirectory(prefix="yukino-terminal-") as task_home:
        config = Path(task_home, ".yukino", "config.yaml")
        config.parent.mkdir()
        config.write_text("providers: []\ntheme: auto\nenable_memory: false\n")
        master, slave = pty.openpty()
        original = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
        child_env = os.environ.copy()
        child_env.update(
            HOME=task_home,
            TERM="xterm-256color",
            COLORTERM="truecolor",
            HTTP_PROXY="http://127.0.0.1:1",
            HTTPS_PROXY="http://127.0.0.1:1",
        )
        child_env.pop("COLORFGBG", None)
        child_env.pop("NO_COLOR", None)
        process = subprocess.Popen(
            [str(binary)], stdin=slave, stdout=slave, stderr=slave,
            cwd=task_home, env=child_env,
        )
        output = bytearray()

        def collect(seconds):
            deadline = time.monotonic() + seconds
            while time.monotonic() < deadline:
                ready, _, _ = select.select(
                    [master], [], [], min(0.03, max(0, deadline - time.monotonic()))
                )
                if ready:
                    try:
                        output.extend(os.read(master, 65536))
                    except OSError:
                        break

        try:
            deadline = time.monotonic() + 3
            while b"\x1b[?996n" not in output and time.monotonic() < deadline:
                collect(0.01)
            queried = b"\x1b[?996n" in output
            os.write(master, report)
            collect(0.5)
            os.write(master, b"\x1b[200~hello\x1b[201~")
            collect(0.15)
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 16, 60, 0, 0))
            os.kill(process.pid, signal.SIGWINCH)
            collect(0.1)
            os.write(master, b"\x03")
            collect(0.1)
            os.write(master, b"\x03")
            collect(0.5)
            code = process.wait(timeout=3)
            restored = termios.tcgetattr(slave)
            # PENDIN is a transient kernel reprint flag, not an input-mode setting.
            restored[3] &= ~getattr(termios, "PENDIN", 0)
            original[3] &= ~getattr(termios, "PENDIN", 0)
            return {
                "mode": mode,
                "query_sent": queried,
                "palette_seen": ("38;2;" + accent).encode() in output,
                "exit_code": code,
                "raw_mode_restored": restored == original,
                "terminal_differences": [f"{i}: {before!r} -> {after!r}" for i, (before, after) in enumerate(zip(original, restored)) if before != after],
                "summary_seen": b"Session" in output and b"--resume" in output,
            }
        finally:
            if process.poll() is None:
                process.kill()
                process.wait()
            os.close(master)
            os.close(slave)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    args = parser.parse_args()
    results = [
        check_terminal(args.binary.resolve(), "light", b"\x1b[?997;2n", "23;146;153"),
        check_terminal(
            args.binary.resolve(), "dark",
            b"\x1b]11;rgb:1e1e/1e1e/2e2e\x1b\\", "148;226;213",
        ),
    ]
    print(json.dumps(results))
    assert all(
        r["query_sent"] and r["palette_seen"] and r["exit_code"] == 0
        and r["raw_mode_restored"] and r["summary_seen"]
        for r in results
    ), results
