#!/usr/bin/env python3
"""Refresh bundled subdivision metadata from a pinned MIT-licensed source."""
import ast
import json
from pathlib import Path
import re
from urllib.request import urlopen

COMMIT = "13e7feb32753216511747ff1d96f0d51c560050d"
REPOSITORY = "wooorm/iso-3166"
ROOT = Path(__file__).resolve().parent.parent
BASE = f"https://raw.githubusercontent.com/{REPOSITORY}/{COMMIT}/"


def download(name):
    with urlopen(BASE + name, timeout=60) as response:
        return response.read()


source = download("2.js").decode("utf-8")
literal = r"(?:'(?:\\.|[^'\\])*'|\"(?:\\.|[^\"\\])*\")"
records = []
for block in re.findall(r"\{([^{}]+)\}", source):
    record = {}
    for field in ("code", "name", "parent"):
        match = re.search(r"\b" + field + r":\s*(" + literal + r")", block)
        if match:
            record[field] = ast.literal_eval(match.group(1))
    if len(record) == 3:
        records.append(record)
assert len(records) == len(re.findall(r"\bcode:\s*['\"]", source)), "Unparsed subdivisions"
assert any(r["code"] == "IN-MP" and r["name"] == "Madhya Pradesh" for r in records)

assets = ROOT / "internal" / "assets"
(assets / "subdivisions.json").write_text(
    json.dumps(records, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
)
(ROOT / "licenses" / "iso-3166-MIT.txt").write_bytes(download("license"))
sources_path = assets / "sources.json"
sources = json.loads(sources_path.read_text())
sources = [s for s in sources if s["repository"] != REPOSITORY]
sources.append({"repository": REPOSITORY, "commit": COMMIT, "file": "2.js", "license": "MIT", "transform": "scripts/update-subdivisions.py"})
sources_path.write_text(json.dumps(sources, indent=2) + "\n")
print(f"Bundled {len(records)} subdivisions from {REPOSITORY}@{COMMIT}")
