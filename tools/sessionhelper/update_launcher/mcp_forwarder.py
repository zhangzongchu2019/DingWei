"""Installed as stable sessionhelper/mcp_shim.py; never runs the update guard.

MCP observes the last confirmed current slot, not a pending helper trial. Missing
or invalid state falls back to factory. Selection never creates or locks state.
"""
from __future__ import annotations

import json
import os
from pathlib import Path
import re
import stat
import sys

MAX_JOURNAL_BYTES = 1024 * 1024


def selected_root(mount: Path) -> Path:
    factory = mount / 'factory'
    try:
        state = Path(os.path.expanduser(os.environ.get('SH_STATE_DIR', '~/.dingwei')))
        update = state / 'update'
        if not state.is_absolute() or update.is_symlink():
            return factory
        update = update.resolve()
        journal = update / 'journal.json'
        if journal.is_symlink():
            return factory
        fd = os.open(journal, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            info = os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_JOURNAL_BYTES:
                return factory
            with os.fdopen(fd, 'rb', closefd=False) as stream:
                raw = stream.read(MAX_JOURNAL_BYTES + 1)
        finally:
            os.close(fd)
        if len(raw) > MAX_JOURNAL_BYTES:
            return factory
        data = json.loads(raw)
        if not isinstance(data, dict) or data.get('format') != 1:
            return factory
        current = data.get('current')
        sha = current.get('sha256') if isinstance(current, dict) else None
        if not isinstance(sha, str) or not re.fullmatch(r'[0-9a-f]{64}', sha):
            return factory
        slot = update / 'slots' / sha
        if slot.resolve() != slot or slot.is_symlink():
            return factory
        package = slot / 'sessionhelper'
        if package.is_symlink() or any(
                (package / name).is_symlink() or not (package / name).is_file()
                for name in ('__init__.py', '__main__.py', 'mcp_shim.py')):
            return factory
        return slot
    except (OSError, ValueError, TypeError, RuntimeError):
        return factory


def main() -> None:
    mount = Path(__file__).resolve().parents[1]
    selected = selected_root(mount)
    # A fresh interpreter removes the already-imported stable sessionhelper
    # package. Explicit path insertion also works when cwd is the mounted root.
    trampoline = ('import runpy,sys;sys.path.insert(0,sys.argv.pop(1));'
                  'runpy.run_module("sessionhelper.mcp_shim",run_name="__main__",alter_sys=True)')
    env = dict(os.environ, PYTHONPATH=str(selected), PYTHONDONTWRITEBYTECODE='1')
    os.execve(sys.executable, [sys.executable, '-B', '-c', trampoline,
                             str(selected), *sys.argv[1:]], env)


if __name__ == '__main__':
    main()
