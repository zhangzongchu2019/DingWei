"""Verify docs-only follow-up from Git objects; no binary or daemon invocation."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

HERE = 'docs/reviews/coder-helper-stale-thread-followup-20261022'
BASE = '13af901d62e83642e840968028fc877f8e42d6d6'
SOURCE = '70358a01e2002ff05e001ce5ff702c238c80f984'
ref = sys.argv[1] if len(sys.argv) > 1 else 'HEAD'
repo = sys.argv[2] if len(sys.argv) > 2 else '.'
def git(*args):
    return subprocess.check_output(['git', '-C', repo, *args])
def blob(path):
    return git('show', ref + ':' + path)
changed = [p for p in git('diff', '--name-only', '-z', BASE, ref).decode().split('\0') if p]
assert changed and all(p.startswith('docs/') for p in changed), changed
assert not git('diff', SOURCE, ref, '--', '.', ':(exclude)docs'), 'approved non-doc bytes changed'
manifest = json.loads(blob(HERE + '/MANIFEST.json'))
for f in manifest['files']:
    data = blob(f['path'])
    assert len(data) == f['bytes'], f['path']
    assert hashlib.sha256(data).hexdigest() == f['sha256'], f['path']
listed = {f['path'] for f in manifest['files']}
actual = {p for p in git('ls-tree', '-rz', '--name-only', ref, '--', HERE).decode().split('\0') if p}
assert actual == (listed - {'docs/designs/S-HELPER-STALE-THREAD-FIX.md'}) | {HERE + '/MANIFEST.json'}
old = 'docs/reviews/coder-helper-stale-thread-fix-20261022/VERIFY.py'
subprocess.run([sys.executable, '-c', blob(old).decode(), ref, repo], check=True)
print('FOLLOWUP_VERIFY_OK files=' + str(len(manifest['files'])) + ' docs_only=True approved_source_unchanged=True')
