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
import shlex
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


def check_entry(inspection):
    """Recognize the supported unchanged entry; unknown launchers fail closed."""
    cfg = inspection.get('Config', {})
    argv = cfg.get('Entrypoint', []) + cfg.get('Cmd', [])
    if not argv or argv[0] not in ('/usr/bin/tini', '/sbin/tini', '/tini', 'tini'):
        raise BootstrapError('entry_unsupported')
    argv = argv[1:]
    while argv and argv[0] in ('-g', '--'):
        argv = argv[1:]
    if len(argv) != 3 or argv[0] not in ('bash', '/bin/bash', '/usr/bin/bash') or argv[1] not in ('-c', '-lc'):
        raise BootstrapError('entry_unsupported')
    # shlex's default wordchars split paths; whitespace_split preserves shell words.
    lex = shlex.shlex(argv[2], posix=True, punctuation_chars=';&|')
    lex.whitespace_split = True
    tokens = list(lex)
    commands, current = [], []
    for token in tokens:
        if token in (';', '&&'):
            commands.append(current); current = []
        else:
            current.append(token)
    commands.append(current)
    env = dict(value.split('=', 1) for value in cfg.get('Env', []) if '=' in value)
    pythonpath = env.get('PYTHONPATH')
    sourced = False
    for command in commands[:-1]:
        if command in (['source', '/run/secrets/sessionhelper_env'], ['.', '/run/secrets/sessionhelper_env']):
            sourced = True
        elif command in (['set', '-a'], ['set', '+a']):
            pass
        elif command == ['export', 'PYTHONPATH=/opt/dingwei/sessionhelper-src']:
            pythonpath = '/opt/dingwei/sessionhelper-src'
        else:
            raise BootstrapError('entry_unsupported')
    final = commands[-1]
    if final and final[0] == 'exec':
        final = final[1:]
    if not sourced or final != ['/opt/dingwei/sessionhelper-venv/bin/python', '-m', 'sessionhelper']:
        raise BootstrapError('entry_unsupported')
    if pythonpath is not None and pythonpath != '/opt/dingwei/sessionhelper-src':
        raise BootstrapError('entry_unsupported')


def check_shared_mount(inspection, source):
    """Refuse replacing a source tree mounted by another running container."""
    ids = docker('ps', '-q').decode().split()
    if not ids:
        return
    for other in json.loads(docker('inspect', *ids)):
        if other['Id'] == inspection['Id']:
            continue
        for mount in other.get('Mounts', []):
            other_source = Path(mount['Source']).resolve()
            if source == other_source or source in other_source.parents or other_source in source.parents:
                raise BootstrapError('source_mount_shared')


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
    check_entry(inspection)
    check_shared_mount(inspection, Path(mount).resolve())
    pins = dependency_pins(Path(root) / 'requirements-runtime.lock')
    script = '''import importlib.metadata,json,os,pathlib,sys
pins=json.loads(sys.argv[1])
if any(importlib.metadata.version(k)!=v for k,v in pins.items()): raise SystemExit(31)
if os.environ.get('PYTHONPATH')!='/opt/dingwei/sessionhelper-src': raise SystemExit(35)
p=pathlib.Path(os.path.expanduser(os.environ.get('SH_STATE_DIR','~/.dingwei')))/'update'
if not p.is_absolute() or p.is_symlink(): raise SystemExit(32)
if sys.argv[3]=='1': p.mkdir(mode=0o700,parents=True,exist_ok=True)
if p.exists() and (p.stat().st_uid!=os.getuid() or p.stat().st_mode&0o077 or not p.is_dir()): raise SystemExit(33)
p=p.resolve()
if sys.argv[3]=='1':
 marker=p/('.bootstrap-'+sys.argv[2]);marker.write_text(sys.argv[2]);os.chmod(marker,0o600)
print(str(p))'''
    nonce = uuid.uuid4().hex
    state = in_container(container, script, json.dumps(pins), nonce,
                         '1' if restart_check else '0').decode().strip()
    if restart_check:
        cleanup = '''import pathlib,sys
p=pathlib.Path(sys.argv[1]);marker=p/('.bootstrap-'+sys.argv[2])
if marker.exists(): marker.unlink()'''
        check = '''import os,pathlib,sys
p=pathlib.Path(sys.argv[1]);marker=p/('.bootstrap-'+sys.argv[2])
if marker.read_text()!=sys.argv[2] or p.stat().st_uid!=os.getuid(): raise SystemExit(34)'''
        try:
            docker('restart', container)  # explicit owner --check-restart-persistence only
            in_container(container, check, state, nonce)
        finally:
            # Best effort when the container cannot be reached after failed restart.
            # A leftover nonce never acts as proof; only success writes proof below.
            try:
                in_container(container, cleanup, state, nonce)
            except subprocess.SubprocessError:
                pass
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
