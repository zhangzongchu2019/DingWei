#!/usr/bin/env python3
"""Run the complete, reviewed helper inventory in two clean profiles."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


def inventory(root):
    manifest = root / 'scripts/helper-test-files.txt'
    expected = manifest.read_text().splitlines()
    actual = sorted(p.relative_to(root).as_posix()
                    for p in (root / 'tools/sessionhelper').rglob('test*.py'))
    if len(expected) != len(set(expected)) or set(expected) != set(actual):
        raise ValueError('helper inventory mismatch: missing=' +
                         repr(sorted(set(expected) - set(actual))) +
                         ' unreviewed=' + repr(sorted(set(actual) - set(expected))))
    if not expected:
        raise ValueError('helper inventory is empty')
    return expected


def run_profile(root, profile, files):
    with tempfile.TemporaryDirectory(prefix='helper-build-', dir='/tmp') as tmp:
        environment = {k: v for k, v in os.environ.items()
                       if not k.startswith(('SH_', 'DW_', 'DINGWEI_', 'DATAD_'))
                       and k not in ('CODEX_HOME', 'CLAUDE_CONFIG_DIR', 'XDG_CONFIG_HOME',
                                     'XDG_STATE_HOME', 'XDG_DATA_HOME', 'XDG_CACHE_HOME')}
        environment.update(HOME=tmp, CODEX_HOME=tmp + '/codex',
                           CLAUDE_CONFIG_DIR=tmp + '/claude',
                           HELPER_TEST_DRIVER=profile,
                           XDG_CONFIG_HOME=tmp + '/config', XDG_STATE_HOME=tmp + '/state',
                           XDG_DATA_HOME=tmp + '/data', XDG_CACHE_HOME=tmp + '/cache',
                           SH_DRIVER=profile, SH_PROVISION_NO_EXIT='1',
                           SH_MCP_IPC_SOCK=tmp + '/ipc.sock',
                           DINGWEI_MCP_IPC_SOCK=tmp + '/ipc.sock',
                           PYTHONPATH=os.pathsep.join([str(root / 'tools/sessionhelper'),
                                                     str(root / 'tools/sessionhelper/tests'),
                                                     str(root / 'view-service')]))
        # Independent discovery count prevents a successful early exit or an
        # accidentally narrowed runner from silently shortening the build gate.
        count_code = '''import unittest
from pathlib import Path
root=Path.cwd();loader=unittest.TestLoader()
suite=loader.discover(str(root/'tools/sessionhelper/tests'))
extra=loader.loadTestsFromName('tools.sessionhelper.test_supervise_connection_loops')
def cases(suite):
 for item in suite:
  if isinstance(item,unittest.TestSuite):yield from cases(item)
  else:yield item
modules={case.__class__.__module__.rsplit('.',1)[-1] for group in (suite,extra) for case in cases(group)}
expected={Path(p).stem for p in (root/'scripts/helper-test-files.txt').read_text().splitlines()}
if expected-modules:raise SystemExit('helper files without collected cases: '+repr(sorted(expected-modules)))
print('HELPER_EXPECTED_CASES='+str(suite.countTestCases()+extra.countTestCases()))
'''
        discovered = subprocess.run([sys.executable, '-c', count_code], cwd=root,
                                    env=environment, text=True, capture_output=True)
        match = re.search(r'^HELPER_EXPECTED_CASES=(\d+)$', discovered.stdout, re.M)
        if discovered.returncode or not match:
            return {'profile': profile, 'rc': 1, 'error': 'helper case discovery failed'}, discovered.stdout + discovered.stderr
        expected = int(match.group(1))
        commands = [
            [sys.executable, 'scripts/test-helper-suite.py', '--driver', profile],
            [sys.executable, '-m', 'unittest', '-v', 'tools/sessionhelper/test_supervise_connection_loops.py'],
        ]
        counts, logs, failed = [], [], False
        for command in commands:
            result = subprocess.run(command, cwd=root, env=environment, text=True,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            logs.append(result.stdout)
            summaries = re.findall(r'^Ran (\d+) tests? in ', result.stdout, re.M)
            counts.append(int(summaries[-1]) if summaries else 0)
            failed |= result.returncode != 0 or not summaries
        total = sum(counts)
        failed |= total != expected
        return {'profile': profile, 'rc': int(failed), 'files': len(files),
                'expected': expected, 'actual': total, 'groups': counts}, ''.join(logs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument('--check-only', action='store_true')
    args = parser.parse_args()
    root = args.root.resolve()
    try:
        files = inventory(root)
    except (ValueError, OSError) as error:
        print('HELPER_GATE_REJECTED ' + str(error), file=sys.stderr)
        return 1
    print(f'HELPER_INVENTORY_OK files={len(files)}', flush=True)
    if args.check_only:
        return 0
    print(f'HELPER_GATE_START profiles=claude,codex python={sys.executable}', flush=True)
    with ThreadPoolExecutor(max_workers=2) as pool:
        futures = [pool.submit(run_profile, root, profile, files) for profile in ('claude', 'codex')]
        reports = []
        for future in futures:
            report, log = future.result()
            print(log, end='', flush=True)
            print('HELPER_GATE_RESULT ' + json.dumps(report, sort_keys=True), flush=True)
            reports.append(report)
    return int(any(report['rc'] for report in reports))


if __name__ == '__main__':
    raise SystemExit(main())
