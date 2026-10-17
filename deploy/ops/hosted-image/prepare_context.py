#!/usr/bin/env python3
"""Prepare an allowlisted offline context from an approved clean Git commit.

No network, Docker, helper execution, installed credentials or runtime state reads.
This script creates only a future build context; development fixtures use synthetic inputs.
"""
import argparse
import base64
import hashlib
import importlib.util
import io
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile
from email.parser import BytesParser

HERE = Path(__file__).resolve().parent
LOCK_SHA = '5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0'


class Rejected(ValueError):
    pass


def regular(path):
    if path.is_symlink() or not path.is_file():
        raise Rejected('input must be a regular non-symlink file')
    return path.read_bytes()


def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args], stderr=subprocess.DEVNULL)


def pins_from_lock(data):
    result, current = {}, None
    for line in data.decode().splitlines():
        match = re.match(r'^([A-Za-z0-9_-]+)==([^\s\\]+)', line)
        if match:
            current = match[1].lower().replace('_', '-')
            if current in result:
                raise Rejected('duplicate runtime pin')
            result[current] = (match[2], set())
        if current:
            result[current][1].update(re.findall(r'--hash=sha256:([0-9a-f]{64})', line))
    if not result or any(not hashes for _, hashes in result.values()):
        raise Rejected('unhashed runtime pin')
    return result


def checked_wheels(directory, lock):
    pins = pins_from_lock(lock)
    seen, blobs = set(), {}
    if directory.is_symlink() or not directory.is_dir():
        raise Rejected('wheelhouse must be a directory')
    for path in directory.iterdir():
        data = regular(path)
        if path.suffix != '.whl':
            raise Rejected('wheelhouse accepts only wheels')
        with zipfile.ZipFile(io.BytesIO(data)) as wheel:
            metadata = [n for n in wheel.namelist() if n.endswith('.dist-info/METADATA')]
            if len(metadata) != 1:
                raise Rejected('invalid wheel metadata')
            meta = BytesParser().parsebytes(wheel.read(metadata[0]))
        name = str(meta['Name']).lower().replace('_', '-')
        if name not in pins or name in seen or meta['Version'] != pins[name][0] or hashlib.sha256(data).hexdigest() not in pins[name][1]:
            raise Rejected('wheel differs from hash lock')
        seen.add(name); blobs[path.name] = data
    if seen != set(pins):
        raise Rejected('wheelhouse incomplete')
    return blobs


def checked_debs(directory, rows):
    if directory.is_symlink() or not directory.is_dir():
        raise Rejected('deb input must be a directory')
    expected = {row['file']: row['sha256'] for row in rows}
    if len(expected) != len(rows) or set(p.name for p in directory.iterdir()) != set(expected):
        raise Rejected('deb set differs from lock')
    result = {}
    for name, sha in expected.items():
        if Path(name).name != name or not name.endswith('.deb'):
            raise Rejected('invalid deb lock path')
        data = regular(directory / name)
        if hashlib.sha256(data).hexdigest() != sha:
            raise Rejected('deb hash mismatch')
        result[name] = data
    return result


def checked_native(path, lock):
    data = regular(path)
    dist = lock['dist']
    integrity = 'sha512-' + base64.b64encode(hashlib.sha512(data).digest()).decode()
    if integrity != dist['integrity']:
        raise Rejected('native tar differs from npm integrity pin')
    files, total = {}, 0
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as tar:
        for member in tar:
            name = PurePosixPath(member.name)
            if name.is_absolute() or '..' in name.parts or not name.parts or name.parts[0] != 'package' or str(name) != member.name.rstrip('/'):
                raise Rejected('unsafe native archive path')
            if member.isdir():
                continue
            if not member.isfile() or member.name in files:
                raise Rejected('native archive links/special/duplicate members forbidden')
            total += member.size
            if total > 1024 * 1024 * 1024:
                raise Rejected('native archive too large')
            files[member.name] = (tar.extractfile(member).read(), member.mode & 0o755)
    if len(files) != dist['fileCount']:
        raise Rejected('native file count differs from lock')
    return files


def source_kit(repo, commit):
    spec = importlib.util.spec_from_file_location('hosted_package_release', repo / 'tools/sessionhelper/package_release.py')
    mod = importlib.util.module_from_spec(spec); spec.loader.exec_module(mod)
    with tempfile.TemporaryDirectory(prefix='hosted-source-kit-') as temp:
        artifact = Path(temp) / 'helper.tgz'
        mod.build(repo, commit, artifact)
        manifest, files = mod.verify(artifact)
    if manifest['version'] != '0.2.3.4' or manifest['commit'] != commit or manifest['lock_sha256'] != LOCK_SHA:
        raise Rejected('helper source version or lock differs from approved release')
    return files


def prepare(repo, commit, wheelhouse, native, runtime_debs, security_debs, output):
    if not re.fullmatch(r'[0-9a-f]{40}', commit) or git(repo, 'rev-parse', 'HEAD').decode().strip() != commit:
        raise Rejected('exact source commit required')
    if git(repo, 'status', '--porcelain'):
        raise Rejected('clean source checkout required')
    if output.exists() or output.is_symlink() or not output.is_absolute() or output.resolve() != output:
        raise Rejected('new absolute context path required')
    prefix = 'deploy/ops/hosted-image/'
    src = lambda name: git(repo, 'show', commit + ':' + prefix + name)
    lock = git(repo, 'show', commit + ':tools/sessionhelper/requirements-runtime.lock')
    if hashlib.sha256(lock).hexdigest() != LOCK_SHA:
        raise Rejected('runtime lock SHA mismatch')
    # Validate every input before creating output. Source exchange remains Git-only.
    helper = source_kit(repo, commit)
    wheels = checked_wheels(wheelhouse, lock)
    runtime = checked_debs(runtime_debs, json.loads(src('runtime-debs-lock.json')))
    security = checked_debs(security_debs, json.loads(src('security-debs-lock.json')))
    native_files = checked_native(native, json.loads(src('codex-package-lock.json')))
    files = {name: (src(name), 0o644) for name in ('Dockerfile', 'start.py', 'codex.py', 'dingwei-file', 'verify_runtime.py')}
    files['.dockerignore'] = (src('dockerignore'), 0o644)
    files.update({'helper/' + n: (b, 0o644) for n, b in helper.items()})
    for folder, blobs in (('wheels', wheels), ('runtime-debs', runtime), ('security-debs', security)):
        files.update({folder + '/' + n: (b, 0o644) for n, b in blobs.items()})
    files.update({'native/' + n: value for n, value in native_files.items()})
    output.mkdir(mode=0o700, parents=False)
    try:
        for name, (data, mode) in files.items():
            dest = output / name; dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(data); dest.chmod(mode)
        receipt = dict(commit=commit, tree=git(repo, 'rev-parse', commit+'^{tree}').decode().strip(),
                       lock_sha256=LOCK_SHA, files={n: hashlib.sha256(v[0]).hexdigest() for n, v in files.items()})
        (output / 'BUILD-INPUTS.json').write_text(json.dumps(receipt, indent=2)+'\n')
    except Exception:
        shutil.rmtree(output)  # only the brand-new private context owned by this invocation
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('repo', 'wheelhouse', 'native-tar', 'runtime-debs', 'security-debs', 'output'):
        parser.add_argument('--'+name, required=True, type=Path)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    try:
        prepare(args.repo, args.commit, args.wheelhouse, args.native_tar, args.runtime_debs, args.security_debs, args.output)
    except Exception:
        parser.exit(1, 'hosted context rejected; no image build permitted\n')


if __name__ == '__main__':
    main()
