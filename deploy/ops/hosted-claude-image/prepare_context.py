#!/usr/bin/env python3
"""Build a new Claude-only context from reviewed offline files; no network/Docker."""
import argparse
import hashlib
import json
import os
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


def _safe_rel(name):
    path = PurePosixPath(name)
    if path.is_absolute() or '..' in path.parts or str(path) in ('', '.'):
        raise Rejected('unsafe H3 receipt path')
    return path


def _h3_files(base):
    receipt = base / 'BUILD-INPUTS.json'
    if receipt.is_symlink() or not receipt.is_file():
        raise Rejected('H3 receipt missing')
    data = json.loads(receipt.read_text())
    files = data.get('files')
    if not isinstance(files, dict) or not files or not isinstance(data.get('commit'), str) or not isinstance(data.get('tree'), str):
        raise Rejected('H3 receipt malformed')
    actual = set()
    for path in base.rglob('*'):
        rel = path.relative_to(base).as_posix()
        if path.is_symlink():
            raise Rejected('H3 context symlink forbidden')
        if path.is_file(): actual.add(rel)
    if actual != set(files) | {'BUILD-INPUTS.json'}:
        raise Rejected('H3 context file set differs from receipt')
    for name, digest in files.items():
        path = base / _safe_rel(name)
        if path.is_symlink() or not path.is_file() or not isinstance(digest, str):
            raise Rejected('H3 receipt member invalid')
        if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise Rejected('H3 receipt hash mismatch')
    required = {'start.py'}
    if not required <= set(files):
        raise Rejected('H3 common guard missing')
    return files, data


def _copy_regular(source, target):
    if source.is_symlink() or not source.is_file():
        raise Rejected('source member must be regular')
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    target.write_bytes(source.read_bytes())
    target.chmod(source.stat().st_mode & 0o755)


def prepare(lock_path, cli_tar, native_tar, node_tar, base, output):
    lock = json.loads(lock_path.read_text())
    verify(lock, cli_tar, native_tar, node_tar)
    if output.exists() or not output.is_absolute():
        raise Rejected('new absolute output required')
    files, h3 = _h3_files(base)
    here = Path(__file__).resolve().parent
    own = ('Dockerfile', 'claude-start.py', 'claude.py', 'dockerignore')
    output.mkdir(mode=0o700)
    try:
        # H3 contributes only its verified common guard and locked common inputs;
        # the final Dockerfile and Claude launchers are selected from this Git tree.
        for name in files:
            if name == 'start.py' or name.startswith(('helper/', 'wheels/', 'runtime-debs/', 'security-debs/')):
                _copy_regular(base / name, output / name)
        for name in own:
            _copy_regular(here / name, output / name)
        unpack_package(cli_tar, output / 'claude', lock['dist']['fileCount'])
        unpack_package(native_tar, output / 'native', lock['native']['fileCount'])
        (output / 'node').mkdir(mode=0o700)
        _copy_regular(node_tar, output / 'node' / lock['runtime']['file'])
        (output / 'BUILD-INPUTS.json').write_text(json.dumps({'h3': h3, 'claude_lock': lock}, sort_keys=True))
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
