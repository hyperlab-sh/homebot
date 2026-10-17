#!/usr/bin/env python3
"""Retarget every release URL of a snapshot cask at the job's loopback server, optionally removing its hook."""

import pathlib
import re
import sys

if len(sys.argv) != 5:
    raise SystemExit("usage: local-cask.py SOURCE DEST BASE with-hook|without-hook")
source, dest, base, hook = sys.argv[1:]
text = pathlib.Path(source).read_text()
remote = "https://github.com/hyperlab-sh/homebot/releases/download"
# GoReleaser writes one url per OS/arch (on_macos/on_linux x on_intel/on_arm),
# so a four-target build has four. Every occurrence must be a cask url stanza,
# or the fixture may rewrite unrelated text. Retarget every one; fail if there
# is none or any release URL survives the rewrite.
urls = re.findall(r'^\s*url\s+"(' + re.escape(remote) + r'/[^"\n]+)"', text, re.M)
if not urls or text.count(remote) != len(urls):
    raise SystemExit(f"expected only cask release URLs, found {text.count(remote)} occurrences and {len(urls)} url stanzas")
text = text.replace(remote, base + "/releases/download")
if remote in text:
    raise SystemExit("release URL survived the rewrite")
if hook == "without-hook":
    # GoReleaser emits the post-install hook as a postflight block. Remove the
    # complete block, including its nested if, rather than disabling xattr by
    # changing the tested artifact.
    lines = text.splitlines(keepends=True)
    starts = [i for i, line in enumerate(lines) if re.fullmatch(r"\s*postflight do\s*\n?", line)]
    if len(starts) != 1:
        raise SystemExit(f"expected one postflight block, found {len(starts)}")
    start = starts[0]
    indent = len(lines[start]) - len(lines[start].lstrip())
    ends = [i for i in range(start + 1, len(lines))
            if lines[i].strip() == "end" and len(lines[i]) - len(lines[i].lstrip()) == indent]
    if not ends:
        raise SystemExit("postflight block has no closing end")
    del lines[start:ends[0] + 1]
    text = "".join(lines)
elif hook != "with-hook":
    raise SystemExit("unknown hook mode")
pathlib.Path(dest).write_text(text)
