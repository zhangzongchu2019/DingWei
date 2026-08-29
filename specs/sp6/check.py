#!/usr/bin/env python3
import pathlib, subprocess, sys

root = pathlib.Path(__file__).resolve().parents[2]
tool = root / "specs/tools/domain-package-check.py"
raise SystemExit(subprocess.call([sys.executable, str(tool), "sp6", *sys.argv[1:]], cwd=root))
