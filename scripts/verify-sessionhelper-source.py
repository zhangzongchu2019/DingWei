#!/usr/bin/env python3
"""Verify the exact member closure of an unpacked development source kit.

The owner must first check the received archive's externally supplied SHA256.
An archive's own manifest is not independent authenticity or release approval.
No dependency install, source execution, network or file mutation occurs here.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat


WRAPPERS = {'SOURCE-MEMBER-PINS.json', 'START-HERE.txt'}
REQUIRED = {'VERSION', 'scripts/verify-sessionhelper-source.py',
            'scripts/record-sessionhelper-macos-build.py',
            'scripts/verify-sessionhelper-frozen.py',
            'tools/sessionhelper/deploy/macos/build-desktop-candidate.sh',
            'tools/sessionhelper/sessionhelper.py',
            'tools/sessionhelper/sessionhelper.spec',
            'tools/sessionhelper/requirements.txt',
            'tools/sessionhelper/sessionhelper/desktop_host.py',
            'tools/sessionhelper/sessionhelper/launch.py',
            'tools/sessionhelper/sessionhelper/setup.py',
            'tools/sessionhelper/sessionhelper/mcp_shim.py'}
MAX_MEMBERS = 1024
MAX_MEMBER_BYTES = 2 * 1024 * 1024


class SourceKitError(ValueError):
    pass


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise SourceKitError('duplicate_manifest_key')
        result[key] = value
    return result


def verify_source(root):
    root = Path(root)
    if root.is_symlink() or not root.is_dir():
        raise SourceKitError('invalid_source_directory')
    root = root.resolve(strict=True)
    members = {}
    # Check every member before reading anything; never block on a FIFO or
    # recurse through directory links. Even unlisted Python/pycache is refused.
    for directory, dirs, files in os.walk(root, followlinks=False):
        for name in dirs + files:
            path = Path(directory) / name
            meta = path.lstat()
            if stat.S_ISDIR(meta.st_mode):
                continue
            if not stat.S_ISREG(meta.st_mode) or meta.st_nlink != 1:
                raise SourceKitError('nonregular_or_linked_source_member')
            if meta.st_size > MAX_MEMBER_BYTES:
                raise SourceKitError('source_member_too_large')
            members[path.relative_to(root).as_posix()] = path
            if len(members) > MAX_MEMBERS:
                raise SourceKitError('too_many_source_members')
    manifest_path = members.get('SOURCE-MEMBER-PINS.json')
    if manifest_path is None:
        raise SourceKitError('source_manifest_missing')
    try:
        manifest = json.loads(manifest_path.read_bytes(), object_pairs_hook=_object)
    except (UnicodeError, json.JSONDecodeError):
        raise SourceKitError('invalid_source_manifest') from None
    if (not isinstance(manifest, dict)
            or not isinstance(manifest.get('source_commit'), str)
            or not re.fullmatch(r'[0-9a-f]{40}', manifest['source_commit'])
            or not isinstance(manifest.get('version'), str)
            or not re.fullmatch(r'[0-9]+(?:\.[0-9]+){2,3}', manifest['version'])
            or not isinstance(manifest.get('files'), list)
            or not 1 <= len(manifest['files']) <= MAX_MEMBERS):
        raise SourceKitError('invalid_source_manifest')
    seen = set()
    for item in manifest['files']:
        if (not isinstance(item, dict) or set(item) != {'path', 'bytes', 'sha256'}
                or not isinstance(item['path'], str)
                or type(item['bytes']) is not int or not 0 <= item['bytes'] <= MAX_MEMBER_BYTES
                or not isinstance(item['sha256'], str)
                or not re.fullmatch(r'[0-9a-f]{64}', item['sha256'])):
            raise SourceKitError('invalid_source_pin')
        name = item['path']
        path = PurePosixPath(name)
        if (not name or path.is_absolute() or '..' in path.parts
                or path.as_posix() != name or '\\' in name
                or name in seen or name in WRAPPERS):
            raise SourceKitError('invalid_source_member_path')
        seen.add(name)
        source = members.get(name)
        if source is None:
            raise SourceKitError('listed_source_member_missing')
        data = source.read_bytes()
        if len(data) != item['bytes'] or hashlib.sha256(data).hexdigest() != item['sha256']:
            raise SourceKitError('source_member_pin_mismatch')
    if not REQUIRED <= seen:
        raise SourceKitError('required_build_member_missing')
    if set(members) != seen | WRAPPERS:
        raise SourceKitError('unlisted_source_member_or_missing_wrapper')
    if members['VERSION'].read_text().strip() != manifest['version']:
        raise SourceKitError('source_version_mismatch')
    return {'status': 'SOURCE_MEMBER_CLOSURE_PASS',
            'source_commit': manifest['source_commit'], 'version': manifest['version'],
            'source_members': len(seen), 'regular_members': len(members),
            'archive_authenticity_verified': False, 'release_ready': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source_root', type=Path)
    args = parser.parse_args()
    try:
        result = verify_source(args.source_root)
    except (SourceKitError, OSError):
        # Do not print unexpected paths or file contents supplied by the kit.
        raise SystemExit('Source kit verification failed; preserve it and check the fixed manifest.') from None
    print(json.dumps(result))


if __name__ == '__main__':
    main()
