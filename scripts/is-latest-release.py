#!/usr/bin/env python3
"""Print whether a candidate is the newest stable tag in git ls-remote input."""
import re
import sys

STABLE = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")


def stable_version(tag):
    match = STABLE.fullmatch(tag)
    return tuple(map(int, match.groups())) if match else None


def is_latest(candidate, remote_lines):
    version = stable_version(candidate)
    if version is None:
        return False
    versions = []
    for line in remote_lines:
        fields = line.split()
        if len(fields) != 2 or not fields[1].startswith("refs/tags/"):
            continue
        parsed = stable_version(fields[1][len("refs/tags/"):])
        if parsed is not None:
            versions.append(parsed)
    # A candidate missing from the remote is not eligible for promotion.
    return bool(versions) and version == max(versions)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: is-latest-release.py VERSION < remote-tags")
    print("true" if is_latest(sys.argv[1], sys.stdin) else "false")
