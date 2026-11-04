#!/usr/bin/env python3
"""Print exactly one version's Markdown body from CHANGELOG.md."""
import argparse
from pathlib import Path
import re
import sys


VERSION = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?")


def release_notes(changelog, version):
    if not VERSION.fullmatch(version):
        raise ValueError("Expected a version tag such as v1.0.0")
    sections = []
    current = None
    fence = None
    for line in changelog.splitlines():
        marker = re.match(r"^\s{0,3}(`{3,}|~{3,})", line)
        if marker:
            delimiter = marker.group(1)
            if fence is None:
                fence = delimiter
            elif delimiter[0] == fence[0] and len(delimiter) >= len(fence):
                fence = None
        heading = re.match(r"^##[ \t]+(.+?)\s*$", line) if fence is None and not marker else None
        if heading:
            current = None
            # Match the complete version token, never v1.0.0 inside v1.0.01/rc.1.
            if re.fullmatch(re.escape(version) + r"(?:[ \t]+[—–-][ \t]+.+)?", heading.group(1)):
                current = []
                sections.append(current)
        elif current is not None:
            current.append(line)
    if len(sections) != 1:
        raise ValueError("Changelog must contain exactly one section for " + version)
    notes = "\n".join(sections[0]).strip()
    if not notes:
        raise ValueError("Changelog section is empty for " + version)
    return notes + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("--changelog", type=Path,
                        default=Path(__file__).resolve().parent.parent / "CHANGELOG.md")
    args = parser.parse_args()
    try:
        notes = release_notes(args.changelog.read_text(encoding="utf-8"), args.version)
    except (OSError, ValueError) as error:
        print(str(error), file=sys.stderr)
        return 1
    sys.stdout.write(notes)
    return 0


if __name__ == "__main__":
    sys.exit(main())
