#!/usr/bin/env python3
"""Runs a command on a pseudo-terminal and answers its questions, or runs it
with no terminal at all. Test helper for the front-door mode.

  pty-run.py [--timeout SECONDS] [--no-tty] [--answer REGEX=TEXT]... -- CMD ARGS...

With a terminal, the command is the session leader of its own session and the
pty is its controlling terminal, so /dev/tty works for it as it does for a
person. Each --answer waits, in order, until REGEX matches the output that came
after the previous answer, then types TEXT and Enter (TEXT may be empty: just
Enter). Output goes to stdout with the carriage returns removed.

--no-tty runs the command in a new session with no controlling terminal, so
opening /dev/tty fails the way it does under `ssh host 'curl ... | bash'`.

Exit status: the command's own; 98 when an --answer never got its prompt; 99
after a timeout (a prompt nobody answered).
"""
import argparse
import os
import pty
import re
import select
import signal
import subprocess
import sys
import time


ANSI = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")


def show(chunks, raw):
    text = "".join(chunks).replace("\r", "")
    return text if raw else ANSI.sub("", text)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--timeout", type=float, default=30.0)
    ap.add_argument("--no-tty", action="store_true")
    ap.add_argument("--raw", action="store_true", help="keep escape codes in the output (answers still match without them)")
    ap.add_argument("--answer", action="append", default=[])
    ap.add_argument("cmd", nargs=argparse.REMAINDER)
    args = ap.parse_args()
    cmd = args.cmd[1:] if args.cmd and args.cmd[0] == "--" else args.cmd
    if not cmd:
        ap.error("no command")

    if args.no_tty:
        try:
            p = subprocess.run(
                cmd,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                start_new_session=True,
                timeout=args.timeout,
            )
        except subprocess.TimeoutExpired as e:
            sys.stdout.write((e.stdout or b"").decode(errors="replace"))
            sys.stdout.write("pty-run: TIMEOUT\n")
            return 99
        sys.stdout.write(p.stdout.decode(errors="replace"))
        return p.returncode

    answers = []
    for a in args.answer:
        regex, sep, text = a.partition("=")
        if not sep:
            ap.error("--answer wants REGEX=TEXT, got %r" % a)
        answers.append((re.compile(regex), text, regex))

    pid, fd = pty.fork()
    if pid == 0:
        os.execvp(cmd[0], cmd)
    buf = ""
    pos = 0
    out = []
    deadline = time.time() + args.timeout
    status = None
    while True:
        if time.time() > deadline:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
            sys.stdout.write(show(out, args.raw))
            sys.stdout.write("\npty-run: TIMEOUT waiting for a prompt (answers left: %s)\n" % [a[2] for a in answers])
            return 99
        r, _, _ = select.select([fd], [], [], 0.1)
        if r:
            try:
                chunk = os.read(fd, 4096)
            except OSError:
                chunk = b""
            if not chunk:
                _, status = os.waitpid(pid, 0)
                break
            s = chunk.decode(errors="replace")
            out.append(s)
            buf += ANSI.sub("", s)
            while answers:
                m = answers[0][0].search(buf, pos)
                if not m:
                    break
                os.write(fd, (answers[0][1] + "\n").encode())
                pos = m.end()
                answers.pop(0)
        else:
            done, st = os.waitpid(pid, os.WNOHANG)
            if done:
                status = st
                # drain what is left
                try:
                    while True:
                        chunk = os.read(fd, 4096)
                        if not chunk:
                            break
                        out.append(chunk.decode(errors="replace"))
                except OSError:
                    pass
                break
    sys.stdout.write(show(out, args.raw))
    if answers:
        sys.stdout.write("pty-run: no prompt matched %s\n" % [a[2] for a in answers])
        return 98
    if os.WIFEXITED(status):
        return os.WEXITSTATUS(status)
    return 128 + os.WTERMSIG(status)


if __name__ == "__main__":
    sys.exit(main())
