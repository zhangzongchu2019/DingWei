"""Local Git-export source packages for the real helper subprocess test.

No remote upload or installed Agent. Candidate version is a synthetic fixture.
"""
import io
import os
import json
import hashlib
import gzip
from pathlib import Path
import subprocess
import sys
import tarfile

repo, revision, work = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
sys.path.insert(0, str(repo / 'tools/sessionhelper'))
import package_release

# Real mixed release mode consumes immutable commits from the shared repository;
# default mode retains the independent synthetic next-version fixture.
factory_revision = os.environ.get('DW_HELPER_E2E_FACTORY_COMMIT')
release_revision = os.environ.get('DW_HELPER_E2E_RELEASE_COMMIT')
if bool(factory_revision) != bool(release_revision):
    raise SystemExit('both source commit pins are required')
if factory_revision:
    base = work / 'factory.tgz'
    base_pin = package_release.build(repo, factory_revision, base)
    candidate = work / 'candidate.tgz'
    pin = package_release.build(repo, release_revision, candidate)
else:
    fixture = work / 'git-source'
    fixture.mkdir()
    raw = subprocess.check_output(['git', '-C', str(repo), 'archive', revision, 'tools/sessionhelper'])
    with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
        archive.extractall(fixture, filter='data')
    def git(*args):
        return subprocess.check_output(['git', '-C', str(fixture), *args], stderr=subprocess.DEVNULL).decode().strip()
    git('init', '-q'); git('add', '.')
    git('-c', 'user.name=synthetic-helper-e2e', '-c', 'user.email=synthetic@invalid.local',
        'commit', '-qm', 'fixture exact source export')
    base = work / 'factory.tgz'
    base_pin = package_release.build(fixture, git('rev-parse', 'HEAD'), base)
    parts = list(map(int, base_pin['version'].split('.'))); parts[-1] += 1
    target = '.'.join(map(str, parts))
    version_file = fixture / 'tools/sessionhelper/sessionhelper/__init__.py'
    version_file.write_text('__version__ = "' + target + '"\n')
    git('add', str(version_file.relative_to(fixture)))
    git('-c', 'user.name=synthetic-helper-e2e', '-c', 'user.email=synthetic@invalid.local',
        'commit', '-qm', 'synthetic next-version candidate')
    candidate = work / 'candidate.tgz'
    pin = package_release.build(fixture, git('rev-parse', 'HEAD'), candidate)
if os.environ.get('DW_HELPER_E2E_NEW_LAYOUT') == '1':
    # A validly transported v2-layout artifact has an extra manifest member.
    # The installed legacy verifier deliberately rejects that layout as
    # manifest_invalid; self_update normalizes it to package_invalid.
    files = {}
    with tarfile.open(candidate, 'r:gz') as archive:
        for member in archive:
            files[member.name] = archive.extractfile(member).read()
    manifest_name = 'helper-release/MANIFEST.json'
    manifest = json.loads(files[manifest_name])
    layout_name = 'layout-v2/layout.json'
    layout_content = b'{"format":2}\n'
    files['helper-release/' + layout_name] = layout_content
    manifest['files'][layout_name] = {'bytes': len(layout_content), 'sha256': hashlib.sha256(layout_content).hexdigest()}
    files[manifest_name] = (json.dumps(manifest, sort_keys=True, indent=2) + '\n').encode()
    candidate.unlink()
    with candidate.open('xb') as dest:
        with gzip.GzipFile(filename='', fileobj=dest, mode='wb', mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w') as archive:
                for name, content in sorted(files.items()):
                    info = tarfile.TarInfo(name)
                    info.size, info.mode, info.mtime = len(content), 0o644, 0
                    archive.addfile(info, io.BytesIO(content))
    pin['sha256'] = hashlib.sha256(candidate.read_bytes()).hexdigest()
    pin['bytes'] = candidate.stat().st_size
_, files = package_release.verify(base, base_pin['sha256'])
mount = work / 'mounted-source'; mount.mkdir()
for name, content in files.items():
    path = mount / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(content)
print(json.dumps({'mount': str(mount), 'artifact': str(candidate), 'pin': pin, 'factory_pin': base_pin}))
