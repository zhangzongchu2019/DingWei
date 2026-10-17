#!/usr/bin/env python3
"""Compose a Git-bound Claude variant of H3. Offline files only; never Docker."""
import argparse
import base64
from email.parser import BytesParser
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
from verify_inputs import Rejected

PREFIX = 'deploy/ops/hosted-claude-image/'
H3 = 'deploy/ops/hosted-image/'
HELPER = 'tools/sessionhelper/'
LIMIT = 1024 * 1024 * 1024
DIR_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def sha(data):
    return hashlib.sha256(data).hexdigest()


def safe_rel(name):
    p = PurePosixPath(name)
    if not isinstance(name, str) or p.is_absolute() or '..' in p.parts or str(p) in ('', '.') or str(p) != name or '\\' in name:
        raise Rejected('noncanonical context path')
    return p


def open_dir(path):
    """Do not follow links in ANY component, including ancestors of the root."""
    path = Path(path)
    if not path.is_absolute() or '..' in path.parts:
        raise Rejected('absolute nonredirected directory required')
    fd = os.open('/', DIR_FLAGS)
    try:
        for part in path.parts[1:]:
            new = os.open(part, DIR_FLAGS, dir_fd=fd)
            os.close(fd)
            fd = new
        return fd
    except BaseException:
        os.close(fd)
        raise


def read_at(fd, name, keep=True, limit=LIMIT):
    stream = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=fd)
    try:
        before = os.fstat(stream)
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_size > limit:
            raise Rejected('regular unaliased input required')
        chunks, digest, size = [], hashlib.sha256(), 0
        while True:
            chunk = os.read(stream, 1 << 20)
            if not chunk:
                break
            size += len(chunk)
            if size > limit:
                raise Rejected('input exceeds budget')
            digest.update(chunk)
            if keep:
                chunks.append(chunk)
        after = os.fstat(stream)
        fields = ('st_dev', 'st_ino', 'st_size', 'st_mtime_ns', 'st_ctime_ns', 'st_nlink', 'st_mode')
        if size != before.st_size or any(getattr(before, x) != getattr(after, x) for x in fields):
            raise Rejected('input changed while reading')
        return b''.join(chunks) if keep else None, digest.hexdigest(), stat.S_IMODE(before.st_mode), size
    finally:
        os.close(stream)


def regular(path, limit=LIMIT):
    path = Path(path)
    fd = open_dir(path.parent)
    try:
        return read_at(fd, path.name, limit=limit)[0]
    finally:
        os.close(fd)


def json_bytes(data):
    def unique(rows):
        d = {}
        for key, value in rows:
            if key in d:
                raise Rejected('duplicate JSON key')
            d[key] = value
        return d
    return json.loads(data, object_pairs_hook=unique)


def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args], stderr=subprocess.DEVNULL)


def git_blob(repo, commit, path):
    safe_rel(path)
    row = git(repo, 'ls-tree', '-z', commit, '--', path).rstrip(b'\0')
    meta, sep, actual = row.partition(b'\t')
    if not sep or actual.decode() != path or meta.split()[:2] not in ([b'100644', b'blob'], [b'100755', b'blob']):
        raise Rejected('Git source must be a regular blob')
    return git(repo, 'cat-file', 'blob', meta.split()[2].decode())


def source_inputs(repo, commit):
    if not re.fullmatch('[0-9a-f]{40}', commit) or git(repo, 'rev-parse', 'HEAD').decode().strip() != commit or git(repo, 'status', '--porcelain', '--untracked-files=all'):
        raise Rejected('exact clean source checkout required')
    mapping_raw = git_blob(repo, commit, PREFIX + 'context-map.json')
    mapping = json_bytes(mapping_raw)
    overlays = {x: PREFIX + y for x, y in [('Dockerfile', 'Dockerfile'), ('claude-start.py', 'claude-start.py'), ('claude.py', 'claude.py'), ('.dockerignore', 'dockerignore')]}
    if set(mapping) != {'format', 'h3', 'common', 'overlay', 'copy_prefixes'} or mapping['format'] != 1 or mapping['overlay'] != overlays or mapping['common'] != {'start.py': H3 + 'start.py'} or mapping['copy_prefixes'] != ['helper/', 'wheels/', 'runtime-debs/', 'security-debs/']:
        raise Rejected('unsupported context map')
    anchor = mapping['h3']
    if set(anchor) != {'commit', 'tree', 'lock_sha256'} or not re.fullmatch('[0-9a-f]{40}', anchor['commit']) or git(repo, 'rev-parse', anchor['commit'] + '^{tree}').decode().strip() != anchor['tree']:
        raise Rejected('H3 source anchor mismatch')
    lock = git_blob(repo, anchor['commit'], HELPER + 'requirements-runtime.lock')
    if sha(lock) != anchor['lock_sha256']:
        raise Rejected('H3 runtime lock differs')
    fixed = {name: git_blob(repo, anchor['commit'], H3 + src) for name, src in [('Dockerfile', 'Dockerfile'), ('start.py', 'start.py'), ('codex.py', 'codex.py'), ('dingwei-file', 'dingwei-file'), ('verify_runtime.py', 'verify_runtime.py'), ('.dockerignore', 'dockerignore')]}
    # Exact in-memory equivalent of package_release's NEW-layout files map.
    # No importing/executing helper, guard or a supplied context's Python code.
    rows = git(repo, 'ls-tree', '-r', '-z', anchor['commit'], '--', HELPER + 'sessionhelper/')
    for row in rows.split(b'\0'):
        if not row:
            continue
        path = row.split(b'\t', 1)[1].decode()
        if path.endswith('.py'):
            fixed['helper/factory/' + path[len(HELPER):]] = git_blob(repo, anchor['commit'], path)
    for dest, src in {'sessionhelper/__main__.py': 'update_launcher/launcher_main.py', 'sessionhelper/mcp_shim.py': 'update_launcher/mcp_forwarder.py', 'helper_update_guard.py': 'update_launcher/guard.py', 'requirements-runtime.lock': 'requirements-runtime.lock', 'bootstrap.py': 'bootstrap.py', 'helper_update_package.py': 'package_release.py'}.items():
        fixed['helper/' + dest] = git_blob(repo, anchor['commit'], HELPER + src)
    fixed['helper/sessionhelper/__init__.py'] = b'"""Stable stdlib launcher; do not replace during self-update."""\n'
    if 'helper/factory/sessionhelper/__init__.py' not in fixed:
        raise Rejected('H3 factory missing')
    debs = {}
    for folder in ['runtime-debs', 'security-debs']:
        rows = json_bytes(git_blob(repo, anchor['commit'], H3 + folder + '-lock.json'))
        for row in rows:
            name = row['file']
            if len(safe_rel(name).parts) != 1 or not name.endswith('.deb') or folder + '/' + name in debs or not re.fullmatch('[0-9a-f]{64}', row['sha256']):
                raise Rejected('H3 deb lock invalid')
            debs[folder + '/' + name] = row['sha256']
    codex_count = json_bytes(git_blob(repo, anchor['commit'], H3 + 'codex-package-lock.json'))['dist']['fileCount']
    return mapping_raw, mapping, fixed, lock, debs, codex_count, {name: git_blob(repo, commit, src) for name, src in overlays.items()}


def h3_snapshot(base, fixed, debs, native_count, anchor):
    fd = open_dir(base)
    try:
        raw = read_at(fd, 'BUILD-INPUTS.json', limit=4 << 20)[0]
        receipt = json_bytes(raw)
        if set(receipt) != {'commit', 'tree', 'lock_sha256', 'files'} or not isinstance(receipt['files'], dict):
            raise Rejected('H3 receipt malformed')
        if {key: receipt[key] for key in ['commit', 'tree', 'lock_sha256']} != anchor:
            raise Rejected('H3 receipt source anchor differs')
        files, dirs, total = {}, set(), 0
        def walk(parent, prefix=''):
            nonlocal total
            for name in sorted(os.listdir(parent)):
                rel = prefix + name
                safe_rel(rel)
                info = os.stat(name, dir_fd=parent, follow_symlinks=False)
                if stat.S_ISDIR(info.st_mode):
                    dirs.add(rel)
                    if len(dirs) > 2048:
                        raise Rejected('directory budget exceeded')
                    child = os.open(name, DIR_FLAGS, dir_fd=parent)
                    try:
                        walk(child, rel + '/')
                    finally:
                        os.close(child)
                    continue
                if rel == 'BUILD-INPUTS.json':
                    if read_at(parent, name, limit=4 << 20)[0] != raw:
                        raise Rejected('receipt changed')
                    continue
                if not stat.S_ISREG(info.st_mode):
                    raise Rejected('links and special files forbidden')
                wheel = rel.startswith('wheels/') and len(PurePosixPath(rel).parts) == 2 and rel.endswith('.whl')
                native = rel.startswith('native/package/')
                if rel not in fixed and rel not in debs and not wheel and not native:
                    raise Rejected('unexpected H3 member')
                value = read_at(parent, name, keep=not native)
                total += value[3]
                if total > LIMIT or len(files) >= 2048:
                    raise Rejected('context budget exceeded')
                files[rel] = value
        walk(fd)
    finally:
        os.close(fd)
    expected_dirs = {str(parent) for name in files for parent in PurePosixPath(name).parents if str(parent) != '.'}
    if dirs != expected_dirs or {n: row[1] for n, row in files.items()} != receipt['files']:
        raise Rejected('H3 tree differs from receipt')
    for name, content in fixed.items():
        if name not in files or files[name][0] != content or files[name][2] != 0o644:
            raise Rejected('H3 Git file mismatch')
    if {n for n in files if n.startswith(('runtime-debs/', 'security-debs/'))} != set(debs) or any(files[n][1] != digest or files[n][2] != 0o644 for n, digest in debs.items()):
        raise Rejected('H3 deb content mismatch')
    if len([n for n in files if n.startswith('native/')]) != native_count:
        raise Rejected('H3 native inventory mismatch')
    return receipt, raw, files


def check_wheels(files, lock):
    pins, current = {}, None
    for line in lock.decode().splitlines():
        match = re.match(r'^([A-Za-z0-9_-]+)==([^\s\\]+)', line)
        if match:
            current = match[1].lower().replace('_', '-')
            if current in pins:
                raise Rejected('duplicate wheel pin')
            pins[current] = (match[2], set())
        if current:
            pins[current][1].update(re.findall(r'--hash=sha256:([0-9a-f]{64})', line))
    if not pins or any(not hashes for _, hashes in pins.values()):
        raise Rejected('unhashed wheel pin')
    seen = set()
    for name, (content, digest, mode, _) in files.items():
        if not name.startswith('wheels/'):
            continue
        with zipfile.ZipFile(io.BytesIO(content)) as archive:
            metadata = [x for x in archive.infolist() if x.filename.endswith('.dist-info/METADATA')]
            if len(metadata) != 1 or metadata[0].file_size > 1 << 20:
                raise Rejected('wheel metadata invalid')
            meta = BytesParser().parsebytes(archive.read(metadata[0]))
        dist = str(meta['Name']).lower().replace('_', '-')
        if dist not in pins or dist in seen or meta['Version'] != pins[dist][0] or digest not in pins[dist][1] or mode != 0o644:
            raise Rejected('wheel differs from H3 hash lock')
        seen.add(dist)
    if seen != set(pins):
        raise Rejected('wheelhouse incomplete')


def unpack_package(blob, prefix, want_count):
    files, total = {}, 0
    with tarfile.open(fileobj=io.BytesIO(blob), mode='r:*') as archive:
        for member in archive:
            name = safe_rel(member.name.rstrip('/') if member.isdir() else member.name)
            if name.parts[0] != 'package':
                raise Rejected('package root invalid')
            if member.isdir():
                continue
            dest = prefix + '/' + str(name)
            if not member.isfile() or dest in files:
                raise Rejected('package link/special/duplicate rejected')
            total += member.size
            if total > LIMIT or len(files) >= 2048:
                raise Rejected('package budget exceeded')
            files[dest] = (archive.extractfile(member).read(), member.mode & 0o755)
    if len(files) != want_count:
        raise Rejected('package file count mismatch')
    return files


def prepare(lock_path, cli_tar, native_tar, node_tar, base, output, *, repo, commit):
    mapping_raw, mapping, fixed, lock, debs, count, overlays = source_inputs(repo, commit)
    approved_lock = git_blob(repo, commit, PREFIX + 'claude-package-lock.json')
    if regular(lock_path, 1 << 20) != approved_lock:
        raise Rejected('Claude lock must equal selected Git source')
    npm = json_bytes(approved_lock)
    if npm.get('name') != '@anthropic-ai/claude-code' or npm.get('version') != '2.1.285' or npm['native'].get('name') != '@anthropic-ai/claude-code-linux-x64' or npm['native'].get('version') != npm['version'] or (npm['runtime'].get('name'), npm['runtime'].get('version'), npm['runtime'].get('platform')) != ('node', '22.23.3', 'linux-x64'):
        raise Rejected('unexpected locked Claude/Node identity')
    if any(type(row['fileCount']) is not int or not 0 < row['fileCount'] <= 2048 for row in [npm['dist'], npm['native']]):
        raise Rejected('invalid archive inventory')
    # Read once without following input aliases; use the same verified bytes.
    cli, native, node = (regular(p) for p in (cli_tar, native_tar, node_tar))
    for data, row in [(cli, npm['dist']), (native, npm['native'])]:
        if 'sha512-' + base64.b64encode(hashlib.sha512(data).digest()).decode() != row['integrity'] or hashlib.sha1(data).hexdigest() != row['shasum']:
            raise Rejected('Claude archive differs from Git lock')
    if Path(node_tar).name != npm['runtime']['file'] or sha(node) != npm['runtime']['sha256']:
        raise Rejected('Node differs from Git lock')
    receipt, receipt_raw, inputs = h3_snapshot(base, fixed, debs, count, mapping['h3'])
    check_wheels(inputs, lock)
    selected = {name: (row[0], row[2]) for name, row in inputs.items() if name == 'start.py' or name.startswith(tuple(mapping['copy_prefixes']))}
    selected.update({name: (data, 0o644) for name, data in overlays.items()})
    selected.update(unpack_package(cli, 'claude', npm['dist']['fileCount']))
    selected.update(unpack_package(native, 'native', npm['native']['fileCount']))
    selected['node/' + npm['runtime']['file']] = (node, 0o644)
    if any(str(parent) in selected for name in selected for parent in safe_rel(name).parents):
        raise Rejected('file/directory collision')
    output = Path(output)
    if Path(base).absolute() in output.parents or Path(repo).absolute() in output.parents:
        raise Rejected('output must not modify input/source tree')
    parent = open_dir(output.parent)
    created = False
    try:
        os.mkdir(output.name, mode=0o700, dir_fd=parent)
        created = True
        root = os.open(output.name, DIR_FLAGS, dir_fd=parent)
        try:
            for name, (data, mode) in selected.items():
                directory = os.dup(root)
                try:
                    parts = safe_rel(name).parts
                    for part in parts[:-1]:
                        try:
                            os.mkdir(part, mode=0o700, dir_fd=directory)
                        except FileExistsError:
                            pass
                        child = os.open(part, DIR_FLAGS, dir_fd=directory)
                        os.close(directory)
                        directory = child
                    dest = os.open(parts[-1], os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode, dir_fd=directory)
                    with os.fdopen(dest, 'wb') as stream:
                        stream.write(data)
                        os.fchmod(stream.fileno(), mode)
                finally:
                    os.close(directory)
            evidence = {'format': 1, 'source': {'commit': commit, 'tree': git(repo, 'rev-parse', commit + '^{tree}').decode().strip()}, 'h3': dict(mapping['h3'], receipt_sha256=sha(receipt_raw)), 'mapping_sha256': sha(mapping_raw), 'claude_lock_sha256': sha(approved_lock), 'files': {n: {'sha256': sha(v[0]), 'mode': v[1], 'type': 'regular'} for n, v in selected.items()}}
            dest = os.open('BUILD-INPUTS.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644, dir_fd=root)
            with os.fdopen(dest, 'w') as stream:
                json.dump(evidence, stream, sort_keys=True, indent=2)
                stream.write('\n')
        finally:
            os.close(root)
    except BaseException:
        if created:
            shutil.rmtree(output.name, dir_fd=parent)
        raise
    finally:
        os.close(parent)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ['lock', 'claude-tar', 'native-tar', 'node-runtime', 'base-context', 'output', 'repo']:
        p.add_argument('--' + name, required=True, type=Path)
    p.add_argument('--commit', required=True)
    a = p.parse_args()
    try:
        prepare(a.lock, a.claude_tar, a.native_tar, a.node_runtime, a.base_context, a.output, repo=a.repo, commit=a.commit)
    except Exception:
        p.exit(2, 'Claude context rejected; no Docker operation permitted\n')


if __name__ == '__main__':
    main()
