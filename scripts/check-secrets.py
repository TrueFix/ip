#!/usr/bin/env python3
"""Prevent accidental tracking of local secrets/data; never print secret values.

This focused guard supplements (does not replace) provider-side key rotation and
GitHub secret scanning. --staged checks index contents, not working-tree edits.
"""
import argparse
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

ASSIGNMENT = re.compile(r"\b(?:MAXMIND_LICENSE_KEY|IPINFO_TOKEN|LicenseKey)\s*[=: ]\s*[\"']?([A-Za-z0-9_-]{10,})")
PLACEHOLDER = re.compile(r"^(?:YOUR_|REPLACE_|EXAMPLE_|TEST_|test[-_]|private-test-)")
PRIVATE_KEY = re.compile(r"-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----")


def forbidden_path(name):
    p = PurePosixPath(name)
    return (
        p.parts[0] in ("data", "secrets", "bin", "dist")
        or (p.name.startswith(".env") and p.name != ".env.example")
        or p.suffix.lower() in (".pem", ".key", ".p12", ".pfx")
        or p.suffix.lower() in (".sqlite", ".db") or ".sqlite-" in p.name or ".db-" in p.name
        or (p.suffix == ".mmdb" and not name.startswith("internal/geo/testdata/"))
    )


def findings(name, data):
    if forbidden_path(name):
        return [f"{name}: local credentials, data, or build output must not be tracked"]
    if name.endswith(".mmdb"):
        return [] if len(data) <= 65536 else [f"{name}: oversized test fixture; check for production data"]
    if b"\0" in data:
        return []
    result = []
    for number, line in enumerate(data.decode("utf-8", errors="replace").splitlines(), 1):
        if PRIVATE_KEY.search(line):
            result.append(f"{name}:{number}: possible private key")
        for match in ASSIGNMENT.finditer(line):
            if not PLACEHOLDER.match(match.group(1)):
                result.append(f"{name}:{number}: possible provider credential")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--staged", action="store_true")
    args = parser.parse_args()
    command = ["git", "ls-files", "-z", "--cached"]
    if not args.staged:
        command.extend(["--others", "--exclude-standard"])
    names = subprocess.check_output(command).decode().split("\0")
    problems = []
    for name in sorted(set(filter(None, names))):
        if args.staged:
            data = subprocess.check_output(["git", "show", ":" + name])
        else:
            path = Path(name)
            if not path.is_file():
                continue
            data = path.read_bytes()
        problems.extend(findings(name, data))
    if problems:
        print("Commit safety checks failed (values redacted):", file=sys.stderr)
        print("\n".join(problems), file=sys.stderr)
        return 1
    print("Secret/data tracking checks passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
