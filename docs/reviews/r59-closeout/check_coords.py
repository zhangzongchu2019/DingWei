#!/usr/bin/env python3
"""核结项表中每个全勾任务的Git证据；只读本机对象库。"""
from pathlib import Path
import re
import subprocess
import sys

def main():
    table = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).with_name('TABLE.md')
    text = table.read_text()
    checks = misses = 0
    rows = {}
    for line in text.splitlines():
        match = re.match(r'\| ([a-z0-9-]+) \| (T-\d+) \| (.+) \|$', line)
        if not match:
            continue
        name, task, evidence = match.groups()
        rows.setdefault(name, set()).add(task)
        coords = re.findall(r'((?:docs|review)/[A-Za-z0-9._/-]+)\s+@\s+([0-9a-f]{40})(?![0-9a-f])', evidence)
        if not coords:
            misses += 1
            print('MISSING coordinate', name, task)
        for path, commit in coords:
            checks += 1
            rc = subprocess.run(['git', 'cat-file', '-e', f'{commit}:{path}'], capture_output=True).returncode
            print('OK' if rc == 0 else 'MISSING', name, task, f'{commit}:{path}')
            misses += rc != 0
    for name, tasks in rows.items():
        paths = list(Path('openspec/changes').glob(f'{name}/tasks.md')) + list(Path('openspec/changes/archive').glob(f'*-{name}/tasks.md'))
        if len(paths) != 1:
            print('MISSING unique-change', name, paths)
            misses += 1
            continue
        source = paths[0].read_text()
        checked = set(re.findall(r'^- \[x\] (T-\d+)\b', source, re.M))
        unchecked = re.findall(r'^\s*- \[ \]', source, re.M)
        if checked != tasks or unchecked:
            print('MISSING task-coverage', name, sorted(checked ^ tasks), len(unchecked))
            misses += 1
    if len(rows) != 10 or checks == 0:
        print('MISSING expected-ten-changes', len(rows))
        misses += 1
    print(f'SUMMARY changes={len(rows)} checked={checks} missing={misses}')
    return int(misses != 0)

if __name__ == '__main__':
    sys.exit(main())
