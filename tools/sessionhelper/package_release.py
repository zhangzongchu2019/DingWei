#!/usr/bin/env python3
"""Build an allowlisted source release from immutable Git objects, never a seat tree."""
from __future__ import annotations

import argparse
import ast
import gzip
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile

PREFIX = 'tools/sessionhelper/'
MAX_BYTES = 32 * 1024 * 1024
FORBIDDEN = re.compile(r'(^|/)(\.env(?:\..*)?|(?:credentials?|secrets?|enroll-token)(?:\..*)?|(?:inbox|ledger)(?:\.(?:db|sqlite|sqlite3|json))?)(/|$)|\.(?:pem|key|p12|pfx|sqlite|sqlite3|db)$', re.I)
SECRET = re.compile(rb'-----BEGIN [A-Z ]*PRIVATE KEY-----|(?:SH_SECRET|SH_KEY_ID|SH_ENROLL_TOKEN|DATAD_TOKEN)\s*=\s*[\x22\x27][^\x22\x27\r\n]+[\x22\x27]')


class PackageError(ValueError):
    pass


def digest(data):
    return hashlib.sha256(data).hexdigest()


def inspect_member(name, content):
    if FORBIDDEN.search(name) or SECRET.search(content):
        raise PackageError('forbidden_private_state')  # never reveal name or matched bytes


def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args], stderr=subprocess.DEVNULL)


def build(repo, commit, output):
    commit = git(repo, 'rev-parse', '--verify', commit + '^{commit}').decode().strip()
    tree = git(repo, 'rev-parse', commit + '^{tree}').decode().strip()
    if git(repo, 'status', '--porcelain', '--untracked-files=all', '--', PREFIX).strip():
        raise PackageError('source_not_clean')
    # Ignored files do not appear in status, but mixing private state into the
    # packaging input must fail even though immutable Git export would omit it.
    local_names = git(repo, 'ls-files', '-z', '--others', '--ignored', '--exclude-standard', '--', PREFIX)
    for raw_name in local_names.split(b'\0'):
        if raw_name and FORBIDDEN.search(raw_name.decode()[len(PREFIX):]):
            raise PackageError('forbidden_private_state')
    listing = git(repo, 'ls-tree', '-r', '-z', commit, '--', PREFIX)
    blobs = {}
    for raw in listing.split(b'\0'):
        if not raw:
            continue
        entry, raw_name = raw.split(b'\t', 1)
        mode, kind, oid = entry.split()
        name = raw_name.decode()
        relative = name[len(PREFIX):]
        # Fail forbidden tracked files even when outside release allowlist.
        if FORBIDDEN.search(relative):
            raise PackageError('forbidden_private_state')
        allowed = ((relative.startswith('sessionhelper/') and relative.endswith('.py')) or
                   relative in ('requirements-runtime.lock', 'bootstrap.py', 'package_release.py',
                                'update_launcher/guard.py', 'update_launcher/launcher_main.py'))
        if not allowed:
            continue
        if mode not in (b'100644', b'100755') or kind != b'blob':
            raise PackageError('unsupported_file_type')
        content = git(repo, 'cat-file', 'blob', oid.decode())
        inspect_member(relative, content)
        blobs[relative] = content
    required = ('sessionhelper/__init__.py', 'sessionhelper/__main__.py', 'requirements-runtime.lock',
                'bootstrap.py', 'package_release.py', 'update_launcher/guard.py', 'update_launcher/launcher_main.py')
    if any(name not in blobs for name in required):
        raise PackageError('release_incomplete')
    version = None
    for node in ast.parse(blobs['sessionhelper/__init__.py']).body:
        if isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == '__version__' for t in node.targets):
            version = ast.literal_eval(node.value)
    if not isinstance(version, str) or not re.fullmatch(r'\d+(?:\.\d+){2,3}', version):
        raise PackageError('version_invalid')
    files = {'factory/' + k: v for k, v in blobs.items() if k.startswith('sessionhelper/')}
    files.update({'sessionhelper/__init__.py': b'"""Stable stdlib launcher; do not replace during self-update."""\n',
                  'sessionhelper/__main__.py': blobs['update_launcher/launcher_main.py'],
                  'helper_update_guard.py': blobs['update_launcher/guard.py'],
                  'requirements-runtime.lock': blobs['requirements-runtime.lock'],
                  'bootstrap.py': blobs['bootstrap.py'],
                  'helper_update_package.py': blobs['package_release.py']})
    manifest = dict(format=1, version=version, commit=commit, tree=tree, platform='linux',
                    lock_sha256=digest(blobs['requirements-runtime.lock']),
                    files={k: dict(bytes=len(v), sha256=digest(v)) for k, v in sorted(files.items())})
    files['MANIFEST.json'] = (json.dumps(manifest, sort_keys=True, indent=2) + '\n').encode()
    if sum(map(len, files.values())) > MAX_BYTES:
        raise PackageError('release_too_large')
    output = Path(output)
    # Do not overwrite another artifact or follow an output symlink.
    with output.open('xb') as dest:
        with gzip.GzipFile(filename='', fileobj=dest, mode='wb', mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w') as archive:
                for name, content in sorted(files.items()):
                    inspect_member(name, content)
                    info = tarfile.TarInfo('helper-release/' + name)
                    info.size, info.mode, info.mtime = len(content), 0o644, 0
                    archive.addfile(info, io.BytesIO(content))
    return {'commit': commit, 'tree': tree, 'version': version, 'sha256': digest(output.read_bytes()),
            'bytes': output.stat().st_size, 'lock_sha256': manifest['lock_sha256']}


def verify(artifact, expected_sha=None):
    artifact = Path(artifact)
    if artifact.stat().st_size > MAX_BYTES:
        raise PackageError('release_too_large')
    if expected_sha and digest(artifact.read_bytes()) != expected_sha:
        raise PackageError('sha256_mismatch')
    files = {}
    total = 0
    with tarfile.open(artifact, 'r:gz') as archive:
        for member in archive:
            path = PurePosixPath(member.name)
            if (not member.isfile() or path.is_absolute() or '..' in path.parts or
                    len(path.parts) < 2 or path.parts[0] != 'helper-release' or
                    str(path) != member.name or member.name in files):
                raise PackageError('archive_invalid')
            total += member.size
            if member.size < 0 or total > MAX_BYTES or len(files) >= 2048:
                raise PackageError('release_too_large')
            content = archive.extractfile(member).read(member.size + 1)
            if len(content) != member.size:
                raise PackageError('archive_invalid')
            name = '/'.join(path.parts[1:])
            if name in files:
                raise PackageError('archive_duplicate')
            inspect_member(name, content)
            files[name] = content
    try:
        manifest = json.loads(files.pop('MANIFEST.json'))
        pins = manifest['files']
        if set(pins) != set(files) or manifest['format'] != 1:
            raise ValueError()
        for name, content in files.items():
            allowed = (name.startswith('factory/sessionhelper/') and name.endswith('.py')) or name in (
                'sessionhelper/__init__.py', 'sessionhelper/__main__.py', 'helper_update_guard.py',
                'bootstrap.py', 'helper_update_package.py', 'requirements-runtime.lock')
            if not allowed or pins[name] != {'bytes': len(content), 'sha256': digest(content)}:
                raise ValueError()
        if digest(files['requirements-runtime.lock']) != manifest['lock_sha256']:
            raise ValueError()
    except (KeyError, ValueError, TypeError):
        raise PackageError('manifest_invalid') from None
    return manifest, files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', default=str(Path(__file__).resolve().parents[2]))
    parser.add_argument('--commit', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(build(args.repo, args.commit, args.output), sort_keys=True))
    except (PackageError, subprocess.CalledProcessError):
        parser.exit(1, 'release packaging rejected\n')


if __name__ == '__main__':
    main()
