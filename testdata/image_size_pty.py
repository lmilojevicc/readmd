#!/usr/bin/env python3
"""Bounded cell-size query/report checks; synthetic PTY, not a graphics emulator.
Usage: python3 testdata/image_size_pty.py /path/to/readmd
Uses a local generated PNG and temporary HOME/XDG directories; never fetches URLs.
"""
import base64
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
import zlib


def png_chunk(kind, data):
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))


def check(binary, supported=True, disabled=False, browser=False):
    with tempfile.TemporaryDirectory(prefix="readmd-image-pty-") as root:
        root = Path(root)
        env = os.environ.copy()
        for name in ("HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"):
            directory = root / name
            directory.mkdir()
            env[name] = str(directory)
        for name in ("KITTY_WINDOW_ID", "WEZTERM_PANE", "GHOSTTY_RESOURCES_DIR", "TMUX"):
            env.pop(name, None)
        env["TERM"] = "xterm-kitty" if supported else "xterm-256color"
        env["GOWORK"] = "off"
        config = root / "XDG_CONFIG_HOME/readmd/config.yaml"
        config.parent.mkdir()
        original = b"style: notty\nmouse: false\nreader_width: 30\nremote_images: false\n"
        config.write_bytes(original)
        pixels = b"\xff\x00\x00\xff" * 800 * 200
        scanlines = (b"\x00" + pixels[:800 * 4]) * 200
        image = (b"\x89PNG\r\n\x1a\n" + png_chunk(b"IHDR", struct.pack(">IIBBBBB", 800, 200, 8, 6, 0, 0, 0))
                 + png_chunk(b"IDAT", zlib.compress(scanlines)) + png_chunk(b"IEND", b""))
        (root / "wide.png").write_bytes(image)
        document = root / "doc.md"
        document.write_text("# ImagePTY\n\n![wide local image](wide.png)\n\nafter\n")
        pid, master = pty.fork()
        if pid == 0:
            argv = [binary, "--no-remote-images"]
            if disabled:
                argv.append("--no-images")
            os.execve(binary, argv + [str(document)], env)
        transcript = bytearray()
        query_count = 0
        exited = False
        metrics = (10, 24)

        def resize(columns):
            fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 24, columns, 0, 0))
            os.kill(pid, signal.SIGWINCH)

        def receive_until(token, start=0, seconds=8):
            nonlocal query_count
            deadline = time.monotonic() + seconds
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], max(0, min(0.1, deadline-time.monotonic())))
                if ready:
                    try:
                        data = os.read(master, 65536)
                    except OSError as error:
                        if error.errno == errno.EIO:
                            break
                        raise
                    if not data:
                        break
                    old = len(transcript)
                    transcript.extend(data)
                    # Include a short overlap so split control sequences are answered once.
                    for query, response in ((b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?1;2c"),
                                            (b"\x1b[16t", f"\x1b[6;{metrics[1]};{metrics[0]}t".encode())):
                        overlap = bytes(transcript[max(0, old-len(query)+1):])
                        for _ in range(overlap.count(query)):
                            if query == b"\x1b[16t":
                                query_count += 1
                                os.write(master, b"\x1b[6;0;0t")  # Bad report must not disable fallback.
                            os.write(master, response)
                if token in transcript[start:]:
                    return
            raise AssertionError(f"timeout waiting for {token!r}; tail={bytes(transcript[-2000:])!r}")

        try:
            resize(40)
            if supported and not disabled:
                receive_until(b"U=1,c=38,r=4,a=p")
                assert query_count > 0, "no cell-size query"
                # A report without SIGWINCH must still invalidate the rendered geometry.
                start = len(transcript)
                metrics = (14, 30)
                os.write(master, b"\x1b[6;30;14t")
                receive_until(b"U=1,c=38,r=5,a=p", start)
                start = len(transcript)
                previous_queries = query_count
                resize(80)
                receive_until(b"U=1,c=57,r=7,a=p", start)
                assert query_count > previous_queries, "resize did not query cell pixels again"
                start = len(transcript)
                os.write(master, b"r")
                receive_until(b"U=1,c=28,r=4,a=p", start)
            else:
                receive_until(b"wide local image")
                assert query_count == 0, "graphics disabled/unsupported but cell query sent"
            if browser:
                start = len(transcript)
                os.write(master, b"\x06")
                receive_until(b"1 document", start)
                receive_until(b"a=d", start)
                # A cell report forces a hidden render; no pixels/placements may
                # escape over the browser, even with asynchronous commands queued.
                metrics = (14, 60)
                os.write(master, b"\x1b[6;60;14t")
                deadline = time.monotonic() + .5
                while time.monotonic() < deadline:
                    ready, _, _ = select.select([master], [], [], max(0, deadline-time.monotonic()))
                    if ready:
                        transcript.extend(os.read(master, 65536))
                hidden = transcript[start:]
                assert b"a=d" in hidden, "browser did not clear Kitty graphics"
                assert b"a=t" not in hidden and b"a=p" not in hidden, "graphics over browser"
                start = len(transcript)
                os.write(master, b"\x1b")
                receive_until(b"U=1,c=28,r=2,a=p", start)
            os.write(master, b"q")
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], 0.1)
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
            assert exited, "pager failed to quit"
            assert config.read_bytes() == original, "config was changed"
            controls = re.findall(rb"\x1b_G(.*?)\x1b\\", transcript, re.DOTALL)
            transmissions = [c for c in controls if c.startswith(b"a=t,")]
            if supported and not disabled:
                assert len(transmissions) == (2 if browser else 1), "unexpected pixel retransmission count"
                assert b"f=32,s=800,v=200," in transmissions[0], transmissions[0][:100]
                payload = b"".join(c.split(b";", 1)[1] for c in controls if b";" in c)
                if browser:
                    # Each transmission has its own base64 padding.
                    groups = []
                    for control in controls:
                        if control.startswith(b"a=t,"):
                            groups.append([])
                        if b";" in control:
                            groups[-1].append(control.split(b";", 1)[1])
                    assert [base64.b64decode(b"".join(parts)) for parts in groups] == [pixels, pixels], "RGBA payload changed"
                else:
                    assert base64.b64decode(payload) == pixels, "RGBA payload changed"
            else:
                assert not controls and query_count == 0, "unexpected graphics/query escapes"
            print(f"PASS image PTY supported={supported} disabled={disabled} queries={query_count} browser={browser}")
        finally:
            if not exited:
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
            os.close(master)


if __name__ == "__main__":
    executable = str(Path(sys.argv[1]).resolve())
    for case in ((True, False), (True, True), (False, False), (True, False, True)):
        check(executable, *case)
