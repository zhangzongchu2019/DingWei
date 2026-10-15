#!/usr/bin/env python3
"""Owner-executed manual bootstrap; never called by the helper's self-updater."""
from __future__ import annotations

import argparse
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import uuid


class BootstrapError(RuntimeError):
    pass


def verified_source(root):
    root = Path(root).resolve()
    try:
        manifest = json.loads((root / 'MANIFEST.json').read_text())
        members = list(root.rglob('*'))
        if any(p.is_symlink() for p in members):
            raise ValueError()
        actual = {str(p.relative_to(root)) for p in members if p.is_file()}
        if actual != set(manifest['files']) | {'MANIFEST.json'}:
            raise ValueError()
        for name, pin in manifest['files'].items():
            path = root / name
            if path.is_symlink() or path.resolve() != path or not path.is_file():
                raise ValueError()
            data = path.read_bytes()
            if pin != {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()}:
                raise ValueError()
        if hashlib.sha256((root / 'requirements-runtime.lock').read_bytes()).hexdigest() != manifest['lock_sha256']:
            raise ValueError()
        return manifest
    except (OSError, ValueError, KeyError, TypeError):
        raise BootstrapError('manifest_invalid') from None


def dependency_pins(lock):
    return dict(re.findall(r'^([A-Za-z0-9_-]+)==([^\s\\]+)', Path(lock).read_text(), re.M))


def install(root, mount, backup):
    """Host-side source-only replacement. Does not write container-private state."""
    root, mount, backup = Path(root).resolve(), Path(mount), Path(backup)
    manifest = verified_source(root)
    if (not mount.is_absolute() or mount.is_symlink() or not mount.is_dir() or
            not backup.is_absolute() or backup.exists() or backup == mount or mount in backup.parents):
        raise BootstrapError('manual_required')
    # Backup only replaced source/launcher paths, never keys/env/inbox or the entire seat tree.
    replaced = ('sessionhelper', 'factory', 'helper_update_guard.py', 'helper_update_package.py', 'requirements-runtime.lock', 'MANIFEST.json')
    for name in ('sessionhelper', 'factory'):
        directory = mount / name
        if directory.exists():
            for path in directory.rglob('*'):
                if path.is_symlink() or (path.is_file() and path.suffix not in ('.py', '.pyc')):
                    raise BootstrapError('private_state_in_source')
    backup.mkdir(mode=0o700, parents=True)
    receipt = {'format': 1, 'mount': str(mount), 'version': manifest['version'], 'replaced': [], 'created': []}
    try:
        for name in replaced:
            destination = mount / name
            if destination.exists():
                if destination.is_dir():
                    shutil.copytree(destination, backup / name)
                else:
                    shutil.copyfile(destination, backup / name)
                receipt['replaced'].append(name)
            else:
                receipt['created'].append(name)
        (backup / 'receipt.json').write_text(json.dumps(receipt))
        os.chmod(backup / 'receipt.json', 0o600)
        for name in replaced:
            source, destination = root / name, mount / name
            if destination.is_dir():
                shutil.rmtree(destination)
            if source.is_dir():
                shutil.copytree(source, destination)
            else:
                shutil.copyfile(source, destination)
        return backup / 'receipt.json'
    except Exception:
        restore(backup / 'receipt.json')
        raise


def restore(receipt_path):
    receipt_path = Path(receipt_path)
    receipt = json.loads(receipt_path.read_text())
    mount, backup = Path(receipt['mount']), receipt_path.parent
    allowed = {'sessionhelper', 'factory', 'helper_update_guard.py', 'helper_update_package.py', 'requirements-runtime.lock', 'MANIFEST.json'}
    names = receipt['replaced'] + receipt['created']
    if not set(names) <= allowed or not mount.is_absolute() or mount.is_symlink():
        raise BootstrapError('receipt_invalid')
    for name in names:
        destination = mount / name
        if destination.is_dir():
            shutil.rmtree(destination)
        elif destination.exists():
            destination.unlink()
        if name in receipt['replaced']:
            source = backup / name
            if source.is_dir():
                shutil.copytree(source, destination)
            else:
                shutil.copyfile(source, destination)


def docker(*args):
    return subprocess.check_output(['docker', *args], stderr=subprocess.DEVNULL, timeout=60)


def in_container(container, script, *args):
    # Read the same env as the existing entry, without printing or copying it.
    return docker('exec', container, 'bash', '-c',
                  'set -a; source /run/secrets/sessionhelper_env; exec "$@"', 'helper-bootstrap',
                  '/opt/dingwei/sessionhelper-venv/bin/python', '-c', script, *args)


def preflight(root, mount, container, restart_check=False):
    manifest = verified_source(root)
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', container):
        raise BootstrapError('container_invalid')
    inspection = json.loads(docker('inspect', container))[0]
    mounts = inspection['Mounts']
    matching = [m for m in mounts if m['Destination'] == '/opt/dingwei/sessionhelper-src']
    if (len(matching) != 1 or matching[0]['RW'] or Path(matching[0]['Source']).resolve() != Path(mount).resolve()
            or inspection['HostConfig']['RestartPolicy']['Name'] != 'unless-stopped'):
        raise BootstrapError('manual_required')
    pins = dependency_pins(Path(root) / 'requirements-runtime.lock')
    script = '''import importlib.metadata,json,os,pathlib,sys
pins=json.loads(sys.argv[1])
if any(importlib.metadata.version(k)!=v for k,v in pins.items()): raise SystemExit(31)
p=pathlib.Path(os.path.expanduser(os.environ.get('SH_STATE_DIR','~/.dingwei')))/'update'
if not p.is_absolute() or p.is_symlink(): raise SystemExit(32)
p.mkdir(mode=0o700,parents=True,exist_ok=True)
if p.stat().st_uid!=os.getuid() or p.stat().st_mode&0o077: raise SystemExit(33)
marker=p/('.bootstrap-'+sys.argv[2]);marker.write_text(sys.argv[2]);os.chmod(marker,0o600)
print(str(p))'''
    nonce = uuid.uuid4().hex
    state = in_container(container, script, json.dumps(pins), nonce).decode().strip()
    if restart_check:
        docker('restart', container)  # explicit owner --check-restart-persistence only
    check = '''import os,pathlib,sys
p=pathlib.Path(sys.argv[1]);marker=p/('.bootstrap-'+sys.argv[2])
if marker.read_text()!=sys.argv[2] or p.stat().st_uid!=os.getuid(): raise SystemExit(34)
marker.unlink()'''
    in_container(container, check, state, nonce)
    proof_script = """import json,pathlib,sys,time
p=pathlib.Path(sys.argv[1])/'bootstrap-preflight.json'
expected={'container':sys.argv[2],'manifest':sys.argv[3]}
if sys.argv[4]=='1':
 expected['expires']=time.time()+600;p.write_text(json.dumps(expected));p.chmod(0o600)
try:
 proof=json.loads(p.read_text());ok=all(proof.get(k)==v for k,v in expected.items() if k!='expires') and proof['expires']>time.time()
except (OSError,ValueError,KeyError): ok=False
print('1' if ok else '0')"""
    manifest_sha = hashlib.sha256((Path(root) / 'MANIFEST.json').read_bytes()).hexdigest()
    verified = in_container(container, proof_script, state, inspection['Id'], manifest_sha,
                            '1' if restart_check else '0').decode().strip() == '1'
    return {'state': state, 'restart_verified': verified, 'version': manifest['version']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_mutually_exclusive_group(required=True)
    modes.add_argument('--preflight', action='store_true')
    modes.add_argument('--install', action='store_true')
    modes.add_argument('--restore', action='store_true')
    parser.add_argument('--mount')
    parser.add_argument('--container')
    parser.add_argument('--venv')  # compatibility with reviewed runbook; actual interpreter fixed in entry
    parser.add_argument('--state')  # checked against resolved container state, never a host write path
    parser.add_argument('--backup')
    parser.add_argument('--receipt')
    parser.add_argument('--check-restart-persistence', action='store_true')
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    try:
        if args.restore:
            restore(args.receipt)
            print('source restored; owner must restart container and verify old version')
            return
        result = preflight(root, args.mount, args.container, args.check_restart_persistence)
        if args.state and str(Path(args.state)) != result['state']:
            raise BootstrapError('state_mismatch')
        if args.install:
            if not result['restart_verified']:
                raise BootstrapError('restart_preflight_required')
            backup = args.backup or str(Path(args.mount).parent / ('helper-source-backup-' + uuid.uuid4().hex))
            result['receipt'] = str(install(root, args.mount, backup))
        print(json.dumps(result))
    except (BootstrapError, OSError, ValueError, subprocess.SubprocessError):
        parser.exit(1, 'bootstrap rejected; inspect local preflight conditions\n')


if __name__ == '__main__':
    main()
