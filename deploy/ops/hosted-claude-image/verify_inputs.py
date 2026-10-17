#!/usr/bin/env python3
"""Verify only reviewed offline Claude build inputs; never downloads anything."""
import argparse
import base64
import hashlib
import json
from pathlib import Path


class Rejected(ValueError):
    pass


def check_blob(path, row):
    if path.is_symlink() or not path.is_file():
        raise Rejected('offline input must be a regular file')
    data = path.read_bytes()
    integrity = 'sha512-' + base64.b64encode(hashlib.sha512(data).digest()).decode()
    if integrity != row.get('integrity') or hashlib.sha1(data).hexdigest() != row.get('shasum'):
        raise Rejected('offline input hash mismatch')


def check_node(path, row):
    if path.name != row.get('file') or path.is_symlink() or not path.is_file():
        raise Rejected('Node runtime filename/type mismatch')
    if hashlib.sha256(path.read_bytes()).hexdigest() != row.get('sha256'):
        raise Rejected('Node runtime hash mismatch')


def verify(lock, cli_tar, native_tar, node_runtime):
    if lock.get('name') != '@anthropic-ai/claude-code' or lock.get('version') != '2.1.285':
        raise Rejected('unexpected Claude package identity')
    native = lock.get('native', {})
    if native.get('name') != '@anthropic-ai/claude-code-linux-x64' or native.get('version') != lock['version']:
        raise Rejected('unexpected native package identity')
    runtime = lock.get('runtime', {})
    if runtime.get('name') != 'node' or runtime.get('version') != '22.23.3' or runtime.get('platform') != 'linux-x64':
        raise Rejected('Node runtime identity mismatch')
    check_blob(cli_tar, lock.get('dist', {}))
    check_blob(native_tar, native)
    check_node(node_runtime, runtime)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--lock', type=Path, required=True)
    p.add_argument('--claude-tar', type=Path, required=True)
    p.add_argument('--native-tar', type=Path, required=True)
    p.add_argument('--node-runtime', type=Path, required=True)
    a = p.parse_args()
    try:
        verify(json.loads(a.lock.read_text()), a.claude_tar, a.native_tar, a.node_runtime)
    except (OSError, ValueError, json.JSONDecodeError):
        p.exit(2, 'Claude offline inputs rejected; no Docker operation permitted\n')


if __name__ == '__main__':
    main()
