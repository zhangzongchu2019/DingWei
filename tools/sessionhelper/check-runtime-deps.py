#!/usr/bin/env python3
"""Bootstrap-only metadata check; never installs or upgrades packages."""
import importlib.metadata
import re
import sys
from pathlib import Path


def locked_versions(path):
    versions, hashed, current = {}, set(), None
    for raw in Path(path).read_text(encoding="utf-8").splitlines():
        line = raw.strip().rstrip("\\").strip()
        if not line or line.startswith("#"):
            continue
        match = re.fullmatch(r"([A-Za-z0-9_.-]+)==([A-Za-z0-9_.+!-]+)", line)
        if match:
            name, version = match.groups()
            name = re.sub(r"[-_.]+", "-", name).lower()
            if name in versions:
                raise ValueError("duplicate package in runtime lock")
            versions[name], current = version, name
        elif current and re.fullmatch(r"--hash=sha256:[0-9a-f]{64}", line):
            hashed.add(current)
        else:
            raise ValueError("runtime lock must contain only exact hashed pins")
    if not versions or set(versions) != hashed or "pyinstaller" in versions:
        raise ValueError("runtime lock must hash every runtime pin and exclude pyinstaller")
    return versions


def check(path):
    mismatches = []
    for name, expected in locked_versions(path).items():
        try:
            actual = importlib.metadata.version(name)
        except importlib.metadata.PackageNotFoundError:
            actual = "missing"
        if actual != expected:
            mismatches.append(f"{name}: installed={actual}, locked={expected}")
    return mismatches


if __name__ == "__main__":
    try:
        failures = check(sys.argv[1])
    except (OSError, ValueError, IndexError) as exc:
        print(f"Invalid runtime lock: {exc}", file=sys.stderr)
        raise SystemExit(1)
    for failure in failures:
        print(failure, file=sys.stderr)
    raise SystemExit(bool(failures))
