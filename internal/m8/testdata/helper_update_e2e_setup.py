"""Local Git-export source packages for the real helper subprocess test.

No remote upload or installed Agent. The layout-cross fixture loads each
immutable revision's packager from that Git object, rather than manufacturing
an extra member in a package made by the current checkout.
"""
import importlib.util
import io
import os
import json
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
    # Package with the code actually shipped by each revision. Importing the
    # current checkout's packager here would erase the layout compatibility
    # boundary under test.
    def immutable_packager(kind, revision):
        source = subprocess.check_output([
            'git', '-C', str(repo), 'show', revision + ':tools/sessionhelper/package_release.py'
        ])
        path = work / (kind + '_package_release.py')
        path.write_bytes(source)
        spec = importlib.util.spec_from_file_location(kind + '_package_release', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module
    factory_packager = immutable_packager('factory', factory_revision)
    release_packager = immutable_packager('candidate', release_revision)
    base = work / 'factory.tgz'
    base_pin = factory_packager.build(repo, factory_revision, base)
    candidate = work / 'candidate.tgz'
    pin = release_packager.build(repo, release_revision, candidate)
    if os.environ.get('DW_HELPER_E2E_IMMUTABLE_LAYOUT_CROSS') == '1':
        try:
            factory_packager.verify(candidate, pin['sha256'])
        except factory_packager.PackageError as exc:
            if str(exc) != 'manifest_invalid':
                raise
        else:
            raise AssertionError('immutable old verifier accepted immutable new-layout package')
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
if factory_revision:
    _, files = factory_packager.verify(base, base_pin['sha256'])
else:
    _, files = package_release.verify(base, base_pin['sha256'])
mount = work / 'mounted-source'; mount.mkdir()
for name, content in files.items():
    path = mount / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(content)
print(json.dumps({'mount': str(mount), 'artifact': str(candidate), 'pin': pin, 'factory_pin': base_pin}))
