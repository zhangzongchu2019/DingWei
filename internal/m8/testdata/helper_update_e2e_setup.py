"""Local Git-export source packages for the real helper subprocess test.

No remote upload or installed Agent. Candidate version is a synthetic fixture.
"""
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile

repo, revision, work = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
sys.path.insert(0, str(repo / 'tools/sessionhelper'))
import package_release

fixture = work / 'git-source'
fixture.mkdir()
raw = subprocess.check_output(['git', '-C', str(repo), 'archive', revision, 'tools/sessionhelper'])
with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
    archive.extractall(fixture, filter='data')

def git(*args):
    return subprocess.check_output(['git', '-C', str(fixture), *args], stderr=subprocess.DEVNULL).decode().strip()

git('init', '-q')
git('add', '.')
git('-c', 'user.name=synthetic-helper-e2e', '-c', 'user.email=synthetic@invalid.local',
    'commit', '-qm', 'fixture exact source export')
base_commit = git('rev-parse', 'HEAD')
base = work / 'factory.tgz'
base_pin = package_release.build(fixture, base_commit, base)
_, files = package_release.verify(base, base_pin['sha256'])
mount = work / 'mounted-source'
mount.mkdir()
for name, content in files.items():
    path = mount / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(content)
version_file = fixture / 'tools/sessionhelper/sessionhelper/__init__.py'
version_file.write_text('__version__ = "0.2.3.2"\n')
git('add', str(version_file.relative_to(fixture)))
git('-c', 'user.name=synthetic-helper-e2e', '-c', 'user.email=synthetic@invalid.local',
    'commit', '-qm', 'synthetic candidate version')
candidate = work / 'candidate.tgz'
pin = package_release.build(fixture, git('rev-parse', 'HEAD'), candidate)
print(json.dumps({'mount': str(mount), 'artifact': str(candidate), 'pin': pin, 'factory_pin': base_pin}))
