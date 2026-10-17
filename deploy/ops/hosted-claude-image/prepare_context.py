#!/usr/bin/env python3
"""Build a new Claude-only context from reviewed offline files; no network/Docker."""
import argparse
import json
from pathlib import Path, PurePosixPath
import shutil
import sys
import tarfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
from verify_inputs import Rejected, verify


def unpack_package(blob, target, want_count):
    count = 0
    with tarfile.open(blob, mode='r:*') as archive:
        for member in archive:
            name = PurePosixPath(member.name)
            if name.is_absolute() or '..' in name.parts or not name.parts or name.parts[0] != 'package':
                raise Rejected('unsafe package path')
            if member.isdir():
                continue
            if not member.isfile():
                raise Rejected('package link/special file rejected')
            count += 1
            path = target / name
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            path.write_bytes(archive.extractfile(member).read())
            path.chmod(member.mode & 0o755)
    if count != want_count:
        raise Rejected('package file count mismatch')


def prepare(lock_path, cli_tar, native_tar, node_tar, base, output):
    lock = json.loads(lock_path.read_text())
    verify(lock, cli_tar, native_tar, node_tar)
    if output.exists() or not output.is_absolute():
        raise Rejected('new absolute output required')
    # The common H3 context must already have been produced by its own verifier.
    # Copy only its explicit allowlist, never an arbitrary source/work directory.
    allow = ('Dockerfile', 'helper', 'wheels', 'runtime-debs', 'security-debs',
             'codex_start.py', 'claude-start.py', 'claude.py')
    for name in allow:
        if not (base / name).exists():
            raise Rejected('base context incomplete')
    output.mkdir(mode=0o700)
    try:
        for name in allow:
            src, dst = base / name, output / name
            if src.is_dir(): shutil.copytree(src, dst, symlinks=False)
            else: shutil.copy2(src, dst, follow_symlinks=False)
        unpack_package(cli_tar, output / 'claude', lock['dist']['fileCount'])
        unpack_package(native_tar, output / 'native', lock['native']['fileCount'])
        (output / 'node').mkdir(mode=0o700)
        shutil.copy2(node_tar, output / 'node' / lock['runtime']['file'])
    except Exception:
        shutil.rmtree(output)
        raise


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--lock', required=True, type=Path)
    p.add_argument('--claude-tar', required=True, type=Path)
    p.add_argument('--native-tar', required=True, type=Path)
    p.add_argument('--node-runtime', required=True, type=Path)
    p.add_argument('--base-context', required=True, type=Path)
    p.add_argument('--output', required=True, type=Path)
    a = p.parse_args()
    try:
        prepare(a.lock, a.claude_tar, a.native_tar, a.node_runtime, a.base_context, a.output)
    except (OSError, ValueError, tarfile.TarError, json.JSONDecodeError):
        p.exit(2, 'Claude context rejected; no Docker operation permitted\n')


if __name__ == '__main__':
    main()
