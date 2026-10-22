"""Verify frozen source + evidence bytes directly from Git, not worktree reads.

Usage: python VERIFY.py [candidate-ref] [repository-directory]
"""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

HERE = 'docs/reviews/coder-helper-stale-thread-fix-20261022'
candidate = sys.argv[1] if len(sys.argv) > 1 else 'HEAD'
repo = sys.argv[2] if len(sys.argv) > 2 else '.'

def git(*args):
    return subprocess.check_output(['git', '-C', repo, *args])

def blob(ref, path):
    return git('show', ref + ':' + path)

source = json.loads(blob(candidate, HERE + '/SOURCE-MANIFEST.json'))
evidence = json.loads(blob(candidate, HERE + '/EVIDENCE-MANIFEST.json'))
assert git('rev-parse', source['source'] + '^{tree}').decode().strip() == source['tree']
for item in source['files']:
    data = blob(source['source'], item['path'])
    assert len(data) == item['bytes'], item['path']
    assert hashlib.sha256(data).hexdigest() == item['sha256'], item['path']
    assert git('rev-parse', source['source'] + ':' + item['path']).decode().strip() == item['blob'], item['path']
    assert blob(candidate, item['path']) == data, ('candidate source drift', item['path'])
source_paths = {item['path'] for item in source['files']}
current_paths = {p for p in git('ls-tree', '-rz', '--name-only', candidate).decode().split('\0') if p and not p.startswith('docs/')}
assert source_paths == current_paths, 'candidate source file set drift'
for item in evidence['files']:
    data = blob(candidate, HERE + '/' + item['path'])
    assert len(data) == item['bytes'], item['path']
    assert hashlib.sha256(data).hexdigest() == item['sha256'], item['path']
evidence_paths = {item['path'] for item in evidence['files']}
present_evidence = {p[len(HERE)+1:] for p in git('ls-tree', '-rz', '--name-only', candidate, '--', HERE).decode().split('\0') if p}
assert evidence_paths == present_evidence - {'EVIDENCE-MANIFEST.json'}, 'evidence file set drift'
# Evidence cannot claim a different runtime lock or release version.
for path in ['VERSION', 'tools/sessionhelper/requirements-runtime.lock', 'tools/sessionhelper/sessionhelper/__init__.py']:
    assert blob(source['source'], path) == blob(source['base'], path), path
print('VERIFY_OK source_files=' + str(len(source['files'])) + ' evidence_files=' + str(len(evidence['files'])))
