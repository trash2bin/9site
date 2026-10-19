#!/usr/bin/env python3
# Stand-in for the site server in the end-to-end run. It answers just enough
# HTTP for deploy/smoke-urls.txt, it has the two subcommands the deploy
# calls, and it can be told through the state directory to fail on start.
#
# Enough to be real for the scenarios, small enough to read in one screen.

import os
import pathlib
import socket
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

STATE = pathlib.Path(os.environ.get("DATA_DIR", "/var/www/9site/var"))
FAIL_START = STATE / "fail-start"
DB = STATE / "site.db"

# The real server prints one span per digit and pads to a fixed width, so the
# number 7 renders as 00007. Same shape here, or the counter check of the
# deploy would read a shape the site never produces.
DIGITS = 5

PAGE = """<!doctype html>
<html><body>
<section class="hero"><span class="counter"></span>
{digits}</section>
</body></html>"""


def digits_of(n):
    s = str(n).zfill(DIGITS)
    return "".join(f"<span>{d}</span>" for d in s)


def counter():
    try:
        return int((STATE / "visits.dat").read_text().strip())
    except OSError:
        return 1


def visits():
    n = counter() + 1
    (STATE / "visits.dat").write_text(f"{n}\n")
    return n


def code_for_path(path):
    path = path.lstrip("/")
    if path == "":
        return 200
    if path.endswith("/"):
        return 404
    if path.startswith(("var/", "avatars/")):
        return 404
    if path in ("auth/register", "auth/login", "signup", "main.go", "visits.dat"):
        return 404
    if path in ("vault", "reset"):
        return 303
    return 200


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def do_GET(self):
        code = code_for_path(self.path.split("?")[0])
        body = PAGE.format(digits=digits_of(visits())).encode() if code == 200 else b""
        self.send_response(code)
        self.send_header("Content-Length", str(len(body)))
        if code == 303:
            self.send_header("Location", "/studio")
        self.end_headers()
        if body:
            self.wfile.write(body)

    do_POST = do_GET


def notify_ready():
    # What the real binary does over sd_notify when the unit is Type=notify:
    # both sockets are bound by now, so the manager may call it started.
    addr = os.environ.get("NOTIFY_SOCKET")
    if not addr:
        return
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
    try:
        sock.connect(addr)
        sock.sendall(b"READY=1")
    except OSError:
        pass
    finally:
        sock.close()


def check():
    # The startup checks of the real binary: the database must open for
    # writing, the static root must exist, the unit must name the state
    # directory. A database owned by root fails here the same way the real
    # server does, which is how a deploy ends in a restart loop.
    if FAIL_START.exists() and not os.environ.get("E2E_ONLY_CHECK"):
        print("стоп: база: var/site.db: unable to open database file (14)", file=sys.stderr)
        return 14
    try:
        with open(DB, "a"):
            pass
    except OSError as err:
        print(f"стоп: база: {DB}: unable to open database file ({err.errno})", file=sys.stderr)
        return err.errno or 14
    if not os.environ.get("STATIC_ROOT"):
        print("стоп: STATIC_ROOT is not set", file=sys.stderr)
        return 14
    return 0


def backup(dst):
    subprocess.run(["cp", str(DB), dst], check=True)


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "check":
        sys.exit(check())
    if len(sys.argv) > 1 and sys.argv[1] == "backup":
        backup(sys.argv[2])
        sys.exit(0)

    if FAIL_START.exists():
        sys.exit(check())

    # The real binary runs its startup checks before it serves, so a
    # database it cannot open stops the service here.
    code = check()
    if code != 0:
        sys.exit(code)

    # Port 80 is in the smoke list, so both listeners have to be up, and both
    # have to be bound before the manager is told the service is ready.
    srv80 = ThreadingHTTPServer(("127.0.0.1", 80), Handler)
    srv8080 = ThreadingHTTPServer(("127.0.0.1", 8080), Handler)
    threading.Thread(target=srv80.serve_forever, daemon=True).start()
    threading.Thread(target=srv8080.serve_forever, daemon=True).start()
    notify_ready()
    threading.Event().wait()
