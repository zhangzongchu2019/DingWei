"""Validate immutable implementation and exact local evidence directly from Git."""
import hashlib
import json
from pathlib import PurePosixPath
import re
import subprocess
import sys

HERE = 'docs/reviews/coder-mcp-slot-pin-20261022'
ref = sys.argv[1] if len(sys.argv) > 1 else 'HEAD'
repo = sys.argv[2] if len(sys.argv) > 2 else '.'
def git(*args):
    return subprocess.check_output(['git', '-C', repo, *args])
def blob(path):
    return git('show', ref + ':' + path)
def doc(path):
    return blob(HERE + '/' + path)
def check(data, item):
    assert len(data) == item['bytes'] and hashlib.sha256(data).hexdigest() == item['sha256'], item['path']

source = json.loads(doc('SOURCE-MANIFEST.json'))
assert git('rev-parse', source['source'] + '^{tree}').decode().strip() == source['tree']
for item in source['files']:
    data = blob(item['path']); check(data, item)
    assert data == git('show', source['source'] + ':' + item['path']), item['path']
    assert git('rev-parse', source['source'] + ':' + item['path']).decode().strip() == item['blob']
assert {p for p in git('ls-tree', '-rz', '--name-only', ref).decode().split('\0') if p and not p.startswith('docs/')} == {i['path'] for i in source['files']}
proof = json.loads(doc('INPUT-PROOF.json'))
assert proof['source'] == source['source'] and proof['base'] == source['base']
assert [p for p in git('diff', '--name-only', source['base'], source['source']).decode().splitlines() if not p.startswith('docs/')] == proof['only_non_docs_changes']
for path in proof['unchanged_paths']:
    assert not git('diff', source['base'], source['source'], '--', path), path
for item in proof['legacy_inputs']:
    check(git('show', proof['legacy_anchor'] + ':' + item['path']), item)
    assert git('rev-parse', proof['legacy_anchor'] + ':' + item['path']).decode().strip() == item['blob']
for item in json.loads(doc('DOC-MANIFEST.json'))['files']:
    check(blob(item['path']), item)
manifest = json.loads(doc('EVIDENCE-MANIFEST.json'))
for item in manifest['files']:
    assert not PurePosixPath(item['path']).is_absolute() and '..' not in PurePosixPath(item['path']).parts
    check(doc(item['path']), item)
present = {p[len(HERE) + 1:] for p in git('ls-tree', '-rz', '--name-only', ref, '--', HERE).decode().split('\0') if p}
assert present == {i['path'] for i in manifest['files']} | {'EVIDENCE-MANIFEST.json'}
for name in ('canonical', 'helper-final'):
    assert doc('raw/gates/' + name + '.rc').strip() == b'0', name
    env = json.loads(doc('raw/gates/' + name + '.json'))
    assert env['source'] == source['source'] and env['env_i'] and env['isolated_HOME']
assert 'BUILD_TESTS_OK python=58 shell=2' in doc('raw/gates/canonical.stdout').decode()
helper = doc('raw/gates/helper-final.stderr').decode()
match = re.search(r'^Ran (\d+) tests in ([\d.]+)s$', helper, re.M)
assert match and int(match[1]) == 824, 'incomplete helper discovery'
assert re.search(r'^OK$', helper, re.M), 'helper not fully green'
assert not re.search(r'^(FAIL|ERROR):', helper, re.M)
assert len(re.findall(r'^test_.* \(test_legacy_mcp_entry\.', helper, re.M)) == 14
prior = doc('raw/gates/helper-full.stderr').decode()
assert doc('raw/gates/helper-full.rc').strip() == b'1'
assert 'FAILED (errors=27)' in prior and 'Ran 469 tests' in prior
assert 'ModuleNotFoundError: No module named \'sessionhelper\'' in prior
checks = json.loads(doc('raw/mutations/CHECKS.json'))
assert checks['source'] == source['source']
rows = checks['rows']; red = [r for r in rows if r['marker']]
assert len(rows) == 34 and len(red) == 16
for row in rows:
    stderr = doc('raw/mutations/' + row['name'] + '.stderr').decode()
    assert doc('raw/mutations/' + row['name'] + '.rc').strip() == str(row['rc']).encode()
    if row['marker']:
        assert row['rc'] == 1 and row['assertion_red'] and row['marker'] in stderr
        assert 'AssertionError' in stderr and 'FAIL:' in stderr and not re.search(r'^ERROR:', stderr, re.M)
    else:
        assert row['rc'] == 0 and re.search(r'^OK$', stderr, re.M)
result = json.loads(doc('raw/mutations/RESULT.json'))
assert result == {'source': source['source'], 'mutations': 16, 'commands': 34,
                  'result': 'LOCAL_INERT_PASS_NOT_SEAT_ACCEPTANCE'}
env = json.loads(doc('raw/gates/ENVIRONMENT.json'))
assert env['uid'] != 0 and not env['deployment'] and env['source'] == source['source']
assert env['runtime_lock_sha256'] == '5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0'
print('VERIFY_OK source_files=' + str(len(source['files'])) + ' evidence_files=' + str(len(manifest['files']))
      + ' helper=824 canonical=58+2 mutations=16 deployment=False')
