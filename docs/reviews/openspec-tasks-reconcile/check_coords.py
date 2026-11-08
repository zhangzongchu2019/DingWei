#!/usr/bin/env python3
"""Verify evidence coordinates cited in TABLE.md against the local Git object DB."""
from pathlib import Path
import re
import subprocess
import sys

TABLE = Path(__file__).with_name("TABLE.md")
DOC_COORD = re.compile(r"(docs/reviews/[A-Za-z0-9._/-]+)\s+@\s+([^\s;|]+)")
RULING_COORD = re.compile(r"ruling\s+(㊲|㊸|㊹|㊺|㊻|㊼|㊽|㊾|㊿)\s+@\s+([^\s;|]+)")

def git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(["git", *args], text=True, capture_output=True)

def main() -> int:
    table = TABLE.read_text(encoding="utf-8")
    checks = 0
    misses = 0
    for path, commit in DOC_COORD.findall(table):
        checks += 1
        if not re.fullmatch(r"[0-9a-f]{40}", commit):
            misses += 1
            print(f"MISSING malformed-commit {commit}:{path}")
            continue
        result = git("cat-file", "-e", f"{commit}:{path}")
        if result.returncode == 0:
            print(f"OK docs {commit}:{path}")
        else:
            misses += 1
            print(f"MISSING docs {commit}:{path}")
    for ruling, commit in RULING_COORD.findall(table):
        checks += 1
        if not re.fullmatch(r"[0-9a-f]{40}", commit):
            misses += 1
            print(f"MISSING malformed-ruling-commit {ruling} @ {commit}")
            continue
        result = git("grep", "-F", ruling, commit, "--", "docs/rulings")
        if result.returncode == 0 and result.stdout.strip():
            matches = [line for line in result.stdout.splitlines() if ruling in line]
            print(f"OK ruling {ruling} @ {commit}: {matches[0]}")
        else:
            misses += 1
            print(f"MISSING ruling {ruling} @ {commit}")
    if checks == 0:
        print("ERROR no evidence coordinates found")
        return 2
    print(f"SUMMARY checked={checks} missing={misses}")
    return 0 if misses == 0 else 1

if __name__ == "__main__":
    sys.exit(main())
