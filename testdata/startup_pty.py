#!/usr/bin/env python3
"""Bounded Unix PTY startup checks. Usage: python3 testdata/startup_pty.py /path/to/readmd
Every process receives temporary HOME/XDG directories; no external URL is opened.
"""
import errno
import fcntl
import os
from pathlib import Path
import pty
import select
import signal
import struct
import sys
import tempfile
import termios
import time


def check(binary, picker, mouse, bootstrap=False):
    with tempfile.TemporaryDirectory(prefix="readmd-pty-") as root:
        root = Path(root)
        env = os.environ.copy()
        for name, subdir in (("HOME", "home"), ("XDG_CONFIG_HOME", "config"),
                             ("XDG_CACHE_HOME", "cache"), ("XDG_STATE_HOME", "state")):
            directory = root / subdir
            directory.mkdir()
            env[name] = str(directory)
        for name in ("KITTY_WINDOW_ID", "WEZTERM_PANE", "GHOSTTY_RESOURCES_DIR", "TMUX"):
            env.pop(name, None)
        env["TERM"] = "xterm-256color"
        env["GOWORK"] = "off"
        config = root / "config/readmd/config.yaml"
        original = None
        if not bootstrap:
            config.parent.mkdir()
            original = (f"# preserve exactly\npicker: {picker}\nmouse: {str(mouse).lower()}\n"
                        "style: notty\nreader: true\nreader_width: 70\ntable_cell_width: 18\n"
                        "images: false\nremote_images: false\n").encode()
            config.write_bytes(original)
        document = root / "doc.md"
        document.write_text("# PTYHeading\n\n[linked](https://example.com/path?q=one#part)\n\n"
                            "1. List item\n   > A blockquote nested inside a list item.\n   > Second line of wisdom.\n")
        pid, master = pty.fork()
        if pid == 0:
            os.execve(binary, [binary, str(document)], env)
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 140, 0, 0))
        transcript = bytearray()
        exited = False

        def receive_until(token, seconds=8):
            deadline = time.monotonic() + seconds
            start = len(transcript)
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], min(0.2, deadline-time.monotonic()))
                if not ready:
                    continue
                try:
                    data = os.read(master, 65536)
                except OSError as error:
                    if error.errno == errno.EIO:
                        break
                    raise
                if not data:
                    break
                transcript.extend(data)
                # Answer terminal capability queries without asserting a graphics protocol.
                if b"\x1b[6n" in data:
                    os.write(master, b"\x1b[1;1R")
                if b"\x1b[c" in data:
                    os.write(master, b"\x1b[?1;2c")
                if token in transcript[start:]:
                    return
            raise AssertionError(f"timeout waiting for {token!r}: {bytes(transcript)!r}")

        try:
            receive_until(b"PTYHeading")
            initial = bytes(transcript)
            captured = b"?1002h" in initial or b"?1003h" in initial
            assert captured == mouse, ("mouse mode", mouse, initial)
            os.write(master, b"p")
            receive_until(b"Pick target" if picker == "list" else b"[A]")
            # Ctrl+C must quit even with a frozen picker active.
            os.write(master, b"\x03")
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], 0.1)
                if ready:
                    try:
                        os.read(master, 65536)
                    except OSError as error:
                        if error.errno != errno.EIO:
                            raise
                done, status = os.waitpid(pid, os.WNOHANG)
                if done:
                    exited = True
                    assert os.waitstatus_to_exitcode(status) == 0, status
                    break
            assert exited, "pager failed to quit within deadline"
            assert config.exists(), "startup did not bootstrap config"
            if original is not None:
                assert config.read_bytes() == original, "startup rewrote config"
            else:
                assert config.stat().st_mode & 0o077 == 0, "bootstrap file is not private"
                assert b"picker: list" in config.read_bytes()
            print(f"PASS PTY picker={picker} mouse={mouse} bootstrap={bootstrap}")
        finally:
            if not exited:
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
            os.close(master)


if __name__ == "__main__":
    executable = str(Path(sys.argv[1]).resolve())
    for case in (("list", True, True), ("list", False, False), ("vimium", False, False), ("vimium", True, False)):
        check(executable, *case)
