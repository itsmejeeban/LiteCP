"""Private PTY bridge: stdin JSON controls, stdout raw terminal bytes."""
import base64
import fcntl
import json
import os
import pty
import select
import signal
import struct
import sys
import termios

pid, master = pty.fork()
if pid == 0:
    os.environ.update(TERM="xterm-256color", COLORTERM="truecolor")
    os.chdir(os.path.expanduser("~"))
    os.execv("/bin/bash", ["bash", "-il"])

def stop(*_):
    try:
        os.killpg(pid, signal.SIGHUP)
    except ProcessLookupError:
        pass
    try:
        os.close(master)
    except OSError:
        pass

signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
pending = b""
try:
    while True:
        ready, _, _ = select.select([master, sys.stdin.buffer], [], [])
        if master in ready:
            try:
                data = os.read(master, 16384)
            except OSError:
                break
            if not data:
                break
            sys.stdout.buffer.write(data)
            sys.stdout.buffer.flush()
        if sys.stdin.buffer in ready:
            data = os.read(sys.stdin.fileno(), 65536)
            if not data:
                break
            pending += data
            while b"\n" in pending:
                line, pending = pending.split(b"\n", 1)
                message = json.loads(line)
                if message.get("data"):
                    data = base64.b64decode(message["data"], validate=True)
                    while data:
                        data = data[os.write(master, data):]
                if message.get("cols") and message.get("rows"):
                    cols = max(20, min(500, int(message["cols"])))
                    rows = max(5, min(200, int(message["rows"])))
                    fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
finally:
    stop()
