"""Stable stdlib-only MCP entry installed outside legacy update slots.

Read one confirmed journal snapshot. Never run the helper/update guard, create
state, acquire a lock, or retry a shim after it has started. Installation is an
explicit operation; this module never edits MCP configuration.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import stat
import sys

MAX_JOURNAL_BYTES = 1024 * 1024
REQUIRED_FILES = ('__init__.py', '__main__.py', 'mcp_shim.py')


def safe_directory(path: Path, *, private: bool = False, factory: bool = False) -> bool:
    """Validate explicit canonical paths without creating or repairing them."""
    info = path.lstat()
    owners = (0, os.getuid()) if factory else (os.getuid(),)
    return (path.is_absolute() and path.resolve() == path
            and stat.S_ISDIR(info.st_mode) and info.st_uid in owners
            and not info.st_mode & (0o077 if private else 0o022))


def valid_package(root: Path, *, factory: bool = False) -> bool:
    if not safe_directory(root, factory=factory):
        return False
    package = root / 'sessionhelper'
    if not safe_directory(package, factory=factory):
        return False
    for name in REQUIRED_FILES:
        info = (package / name).lstat()
        if (not stat.S_ISREG(info.st_mode)
                or info.st_uid not in ((0, os.getuid()) if factory else (os.getuid(),))
                or (factory and info.st_mode & 0o022)):
            return False
    return True


def confirmed_slot(state: Path) -> Path | None:
    try:
        if not safe_directory(state, private=True):
            return None
        update = state / 'update'
        if not safe_directory(update, private=True):
            return None
        fd = os.open(update / 'journal.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                    or info.st_mode & 0o077 or info.st_size > MAX_JOURNAL_BYTES):
                return None
            with os.fdopen(fd, 'rb', closefd=False) as stream:
                raw = stream.read(MAX_JOURNAL_BYTES + 1)
        finally:
            os.close(fd)
        if len(raw) > MAX_JOURNAL_BYTES:
            return None
        data = json.loads(raw)
        if not isinstance(data, dict) or type(data.get('format')) is not int or data['format'] != 1:
            return None
        current = data.get('current')
        sha = current.get('sha256') if isinstance(current, dict) else None
        if not isinstance(sha, str) or not re.fullmatch(r'[0-9a-f]{64}', sha):
            return None
        slots = update / 'slots'
        if not safe_directory(slots):
            return None
        slot = slots / sha
        return slot if valid_package(slot) else None
    except (OSError, ValueError, TypeError, RuntimeError, RecursionError):
        return None


def selected_root(state: Path, factory: Path) -> tuple[Path, bool]:
    slot = confirmed_slot(state)
    if slot is not None:
        return slot, False
    try:
        if valid_package(factory, factory=True):
            return factory, True
    except (OSError, ValueError, TypeError, RuntimeError):
        pass
    raise ValueError('no_valid_mcp_package')


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--state-dir', type=Path, required=True)
    parser.add_argument('--factory-root', type=Path, required=True)
    args = parser.parse_args()
    try:
        selected, fallback = selected_root(args.state_dir, args.factory_root)
    except ValueError:
        sys.stderr.write('mcp_slot_pin: no_valid_mcp_package\n')
        return 78
    if fallback:
        sys.stderr.write('mcp_slot_pin: factory_fallback\n')
    # A fresh interpreter avoids any imported stable/factory package shadowing
    # the confirmed slot. No callback or catch around the exec can replay calls.
    trampoline = ('import sys;sys.path.insert(0,sys.argv.pop(1));import runpy;'
                  'runpy.run_module("sessionhelper.mcp_shim",run_name="__main__",alter_sys=True)')
    env = dict(os.environ, PYTHONPATH=str(selected), PYTHONDONTWRITEBYTECODE='1')
    try:
        os.execve(sys.executable, [sys.executable, '-I', '-B', '-c', trampoline, str(selected)], env)
    except OSError:
        sys.stderr.write('mcp_slot_pin: exec_failed\n')
        return 78


if __name__ == '__main__':
    raise SystemExit(main())
