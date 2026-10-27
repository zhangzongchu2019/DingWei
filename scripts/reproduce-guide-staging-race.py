#!/usr/bin/env python3
"""Foreground, isolated repetitions of the guide duplicate regression."""
import argparse
import ast
import datetime
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BASE = '549f64e280f2c5496bffb506c709c1bf0a1c504d'


def worker(profile, reverse):
    import threading
    import time
    import unittest
    from unittest import mock
    sys.path[:0] = [str(ROOT/'tools/sessionhelper'), str(ROOT/'tools/sessionhelper/tests')]
    import test_guide_staging as module
    original_setup = module.setUpModule

    def setup():
        original_setup()
        os.environ['SH_DRIVER'] = profile

    module.setUpModule = setup
    cls = module.GuideRealAdapterBoundaryTest
    name = 'test_duplicate_releases_claim_for_next_business_command'
    if not reverse:
        result = unittest.TextTestRunner(verbosity=2).run(unittest.TestSuite([cls(name)]))
        return int(not result.wasSuccessful())

    # Reverse only the changed test method to the immutable baseline body.
    source = subprocess.check_output(['git', 'show', BASE+':tools/sessionhelper/tests/test_guide_staging.py'], cwd=ROOT, text=True)
    tree = ast.parse(source)
    method = next(n for c in tree.body if isinstance(c, ast.ClassDef) and c.name == cls.__name__
                  for n in c.body if isinstance(n, ast.FunctionDef) and n.name == name)
    namespace = {}
    exec(compile(ast.Module(body=[method], type_ignores=[]), '<original-guide-assertions>', 'exec'), module.__dict__, namespace)
    setattr(cls, name, namespace[name])
    release = threading.Event()
    adapters = []
    original_deliver = module.FakeProtocolDriver.deliver
    original_make = cls.make_driver_helper
    original_temp = tempfile.TemporaryDirectory

    def deliver(driver, *args):
        receipt = original_deliver(driver, *args)
        # Calls.append has happened; receipt/admission callbacks cannot happen
        # until teardown releases this gate. No probabilistic sleep is used.
        if not release.wait(5):
            raise AssertionError('controlled receipt gate was not released')
        return receipt

    def make(case, *args):
        helper, adapter = original_make(case, *args)
        adapters.append(adapter)
        return helper, adapter

    class Temp(original_temp):
        def __exit__(self, *args):
            release.set()
            deadline = time.monotonic()+3
            while any(a._commands.depth for a in adapters) and time.monotonic() < deadline:
                time.sleep(.001)
            return super().__exit__(*args)

    with mock.patch.object(module.FakeProtocolDriver, 'deliver', deliver), \
            mock.patch.object(cls, 'make_driver_helper', make), \
            mock.patch.object(module.tempfile, 'TemporaryDirectory', Temp):
        result = unittest.TextTestRunner(verbosity=2).run(unittest.TestSuite([cls(name)]))
    if len(result.failures) == 1 and not result.errors and 'Lists differ' in result.failures[0][1] and "'same-1'" in result.failures[0][1]:
        print('EXPECTED_ORIGINAL_RACE callback absent after calls.append', flush=True)
        return 1
    return 2


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--rounds', type=int, default=50)
    parser.add_argument('--driver', choices=('claude', 'codex'), default='codex')
    parser.add_argument('--reverse', action='store_true')
    parser.add_argument('--worker', action='store_true')
    args = parser.parse_args()
    if args.worker:
        return worker(args.driver, args.reverse)
    expected = 1 if args.reverse else 0
    for number in range(1, args.rounds+1):
        with tempfile.TemporaryDirectory(prefix='guide-repeat-', dir='/tmp') as home:
            env = {k:v for k,v in os.environ.items() if not k.startswith(('SH_', 'DW_', 'DINGWEI_', 'DATAD_'))}
            env.update(HOME=home, CODEX_HOME=home+'/codex', CLAUDE_CONFIG_DIR=home+'/claude',
                       SH_MCP_IPC_SOCK=home+'/ipc.sock', DINGWEI_MCP_IPC_SOCK=home+'/ipc.sock',
                       PYTHONDONTWRITEBYTECODE='1', TZ='UTC')
            command = [sys.executable, str(Path(__file__).resolve()), '--worker', '--driver', args.driver]
            if args.reverse:
                command.append('--reverse')
            print(f'ROUND {number}/{args.rounds} driver={args.driver} UTC={datetime.datetime.now(datetime.timezone.utc).isoformat()} cores={os.cpu_count()} load={os.getloadavg()}', flush=True)
            result = subprocess.run(command, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            print(result.stdout, end='', flush=True)
            if result.returncode != expected or (args.reverse and 'EXPECTED_ORIGINAL_RACE' not in result.stdout):
                return 2
    print(f'SUMMARY rounds={args.rounds} unexpected=0 each_rc={expected}', flush=True)
    return expected


if __name__ == '__main__':
    raise SystemExit(main())
