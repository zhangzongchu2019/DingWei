#!/usr/bin/env python3
"""Record native development build metadata; no account/history/Hub access."""
import argparse
import datetime
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import platform
import re
import stat
import subprocess


def create_receipt(source_root, commit, output):
    if platform.system() != 'Darwin' or not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('native Mac and exact source commit required')
    source_root, output = Path(source_root), Path(output)
    if output.is_symlink() or output.stat().st_uid != os.getuid() or output.stat().st_mode & 0o077:
        raise ValueError('private owned build output required')
    before = json.loads((output / 'desktop-metadata-before-build.json').read_bytes())
    after = json.loads((output / 'desktop-metadata.json').read_bytes())
    for item in (before, after):
        if (item.get('platform') != 'Darwin' or len(item.get('applications', [])) != 1
                or item.get('architecture') != platform.machine()
                or item.get('native_auto_notify_implemented') is not False):
            raise ValueError('native app metadata unavailable or inconsistent')
    if before['applications'] != after['applications']:
        raise ValueError('desktop application changed during build; retain evidence')
    smoke = []
    for line in (output / 'local-smoke.jsonl').read_text().splitlines():
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict) and value.get('status') == 'FROZEN_LOCAL_SMOKE_PASS':
            smoke.append(value)
    if len(smoke) != 1:
        raise ValueError('one completed local frozen smoke required')
    version = (source_root / 'VERSION').read_text().strip()
    binary = output / 'bin/sessionhelper'
    metadata = binary.lstat()
    if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1:
        raise ValueError('regular built binary required')
    binary_bytes = binary.read_bytes()
    digest = hashlib.sha256(binary_bytes).hexdigest()
    if (smoke[0].get('version') != version or smoke[0].get('bytes') != len(binary_bytes)
            or smoke[0].get('sha256') != digest or smoke[0].get('real_hub_or_ai_calls') != 0
            or smoke[0].get('native_desktop_runtime') != 'NOT_VERIFIED'):
        raise ValueError('frozen smoke binary pin or qualification mismatch')
    # Read metadata only; never pip-freeze raw direct URLs or environment values.
    dependencies = {name: importlib.metadata.version(name) for name in ('pyinstaller', 'websockets', 'pexpect')}
    if (source_root / '.git').exists():
        actual = subprocess.check_output(['git', '-C', str(source_root), 'rev-parse', 'HEAD'], text=True).strip()
        dirty = subprocess.check_output(['git', '-C', str(source_root), 'status', '--porcelain'], text=True).strip()
        if actual != commit or dirty:
            raise ValueError('source changed during build')
        source_kind = 'clean_git_checkout'
    else:
        # Recheck the complete source after compilation, before recording PASS.
        from importlib.util import spec_from_file_location, module_from_spec
        spec = spec_from_file_location('source_kit_check', source_root / 'scripts/verify-sessionhelper-source.py')
        check = module_from_spec(spec)
        spec.loader.exec_module(check)
        if check.verify_source(source_root)['source_commit'] != commit:
            raise ValueError('source commit mismatch')
        source_kind = 'fixed_source_archive'
    return {'recorded_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'status': 'MAC_DEVELOPMENT_BUILD_LOCAL_SMOKE_PASS',
            'source_commit': commit, 'source_kind': source_kind, 'version': version,
            'architecture': platform.machine(), 'python_version': platform.python_version(),
            'dependencies': dependencies, 'binary': {'bytes': len(binary_bytes), 'sha256': digest},
            'desktop_app': after['applications'][0],
            'signature_verified': False, 'native_desktop_model_runtime': 'NOT_VERIFIED',
            'native_auto_notify_implemented': False, 'real_hub_or_ai_calls': 0, 'release_ready': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-root', type=Path, required=True)
    parser.add_argument('--source-commit', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        receipt = create_receipt(args.source_root, args.source_commit, args.output)
        path = args.output / 'MAC-BUILD-MANIFEST.json'
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'w') as f:
            f.write(json.dumps(receipt, ensure_ascii=False, indent=2) + '\n')
            f.flush(); os.fsync(f.fileno())
        print('Native development build manifest saved; original desktop/model/signing remain unverified.')
    except (OSError, ValueError, subprocess.SubprocessError, importlib.metadata.PackageNotFoundError):
        raise SystemExit('Mac build receipt refused; retain build artifacts and metadata, do not release.') from None


if __name__ == '__main__':
    main()
