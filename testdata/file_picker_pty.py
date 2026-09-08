#!/usr/bin/env python3
"""Isolated file-browser, explicit-file and piped-reader PTYs; no installed app/config."""
import errno
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import sys
import tempfile
import termios
import time


def check(binary, mode):
    with tempfile.TemporaryDirectory(prefix="readmd-files-pty-") as scratch:
        root = Path(scratch)
        env = os.environ.copy()
        for name, subdir in (("HOME", "home"), ("XDG_CONFIG_HOME", "config"),
                             ("XDG_CACHE_HOME", "cache"), ("XDG_STATE_HOME", "state")):
            directory = root / subdir
            directory.mkdir()
            env[name] = str(directory)
        for name in ("KITTY_WINDOW_ID", "WEZTERM_PANE", "GHOSTTY_RESOURCES_DIR", "TMUX"):
            env.pop(name, None)
        env.update(TERM="xterm-256color", GOWORK="off")
        docs = root / "docs"
        docs.mkdir()
        for i in range(30):
            (docs / f"doc{i:02d}.md").write_text(f"# Heading{i:02d}\n\nDocument {i}\n")
        if mode in ("startup alias", "explicit alias"):
            alias = root / "alias"
            alias.symlink_to(docs, target_is_directory=True)
            docs = alias
            env["PWD"] = str(alias)
        args = [binary]
        if mode in ("explicit", "explicit alias", "precedence"):
            args.append(str(docs / "doc00.md"))
        if mode == "dash":
            (docs / "-").write_text("# LiteralDash\n")
            args.append("-")
        pipe = None
        if mode in ("piped", "precedence"):
            pipe = os.pipe()
            os.write(pipe[1], b"# PipedHeading\n\nstdin body\n")
            os.close(pipe[1])
        pid, master = pty.fork()
        if pid == 0:
            os.chdir(docs)
            if pipe:
                os.dup2(pipe[0], 0)
                os.close(pipe[0])
            os.execve(binary, args, env)
        if pipe:
            os.close(pipe[0])
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 110, 0, 0))
        transcript = bytearray()
        exited = False

        def receive(token, start=0, seconds=8):
            deadline = time.monotonic() + seconds
            while time.monotonic() < deadline:
                if token in re.sub(rb"\x1b\[[0-9;]*m", b"", transcript[start:]):
                    return
                ready, _, _ = select.select([master], [], [], min(.2, deadline-time.monotonic()))
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
                if b"\x1b[6n" in data:
                    os.write(master, b"\x1b[1;1R")
                if b"\x1b[c" in data:
                    os.write(master, b"\x1b[?1;2c")
            raise AssertionError(f"{mode}: waiting for {token!r}: {bytes(transcript)!r}")

        def send(keys, token):
            start = len(transcript)
            os.write(master, keys)
            receive(token, start)

        try:
            if mode in ("startup", "startup alias"):
                receive(b"30 documents")
                send(b"l", b"6.")
                send(b"h", b"1.")
                # Enter immediately after text must wait for the latest fuzzy result.
                send(b"/doc17\r", b"Heading17")
                send(b"\x1b", b"doc17.md")
                send(b"\x1b", b"30 documents")
                send(b"\x1b", b"Heading17")
                send(b"\x06", b"30 documents")
                os.write(master, b"q")
            else:
                heading = b"PipedHeading" if mode == "piped" else b"LiteralDash" if mode == "dash" else b"Heading00"
                receive(heading)
                if mode == "precedence":
                    assert b"PipedHeading" not in transcript
                send(b"\x06", b"30 documents")
                send(b"\x1b", heading)
                send(b"\x06", b"30 documents")
                send(b"/doc17\r", b"Heading17")
                send(b"\x1b", b"doc17.md")
                os.write(master, b"\x03")
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], .1)
                if ready:
                    try:
                        transcript.extend(os.read(master, 65536))
                    except OSError as error:
                        if error.errno != errno.EIO:
                            raise
                done, status = os.waitpid(pid, os.WNOHANG)
                if done:
                    exited = True
                    assert os.waitstatus_to_exitcode(status) == 0, status
                    break
            assert exited, "did not quit"
            config = root / "config/readmd/config.yaml"
            assert config.exists(), "missing bootstrap"
            assert config.stat().st_mode & 0o077 == 0, "nonprivate bootstrap"
            assert not (config.parent / "theme.yaml").exists(), "created theme file"
            print(f"PASS file browser PTY mode={mode}")
        finally:
            if not exited:
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
            os.close(master)


if __name__ == "__main__":
    executable = str(Path(sys.argv[1]).resolve())
    for case in ("startup", "startup alias", "explicit", "explicit alias", "piped", "precedence", "dash"):
        check(executable, case)
