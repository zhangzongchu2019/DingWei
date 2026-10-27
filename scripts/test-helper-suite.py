#!/usr/bin/env python3
"""Reproduce the helper suite without inherited live-seat configuration.

Use the locked runtime interpreter to invoke this file. --driver explicitly
selects a profile for tests which inherit SH_DRIVER; it never edits seat files.
"""
import argparse
from pathlib import Path
import os
import subprocess
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'tools/sessionhelper/tests'))
from _suite_isolation import isolated_environment, assert_isolated, isolate_test_cases


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--driver',choices=('all','unset','claude','codex'),default='all')
    parser.add_argument('--pattern',default='test_*.py')
    args=parser.parse_args()
    root=ROOT
    if args.driver=='all':
        results=[subprocess.run([sys.executable,str(Path(__file__).resolve()),'--driver',profile,'--pattern',args.pattern],cwd=root).returncode for profile in ('claude','codex')]
        return 1 if any(results) else 0
    print(f'HELPER_SUITE python={sys.executable} version={sys.version.split()[0]} driver={args.driver} pattern={args.pattern}',flush=True)
    # Discovery/imports and module/class fixtures are isolated too, before any
    # helper module can capture a HOME or socket path at import time.
    with isolated_environment(args.driver) as directory:
        actual = os.environ.get('SH_DRIVER', 'unset')
        print(f'HELPER_PROFILE selected={args.driver} actual_SH_DRIVER={actual}', flush=True)
        assert_isolated(directory, args.driver)
        sys.path[:0] = [str(root/'tools/sessionhelper'), str(root/'view-service')]
        os.environ['PYTHONPATH'] = os.pathsep.join(sys.path[:2])
        os.chdir(root)
        suite = unittest.defaultTestLoader.discover(str(root/'tools/sessionhelper/tests'), pattern=args.pattern)
        with isolate_test_cases(args.driver):
            result = unittest.TextTestRunner(verbosity=2).run(suite)
        return 0 if result.wasSuccessful() else 1


if __name__=='__main__':raise SystemExit(main())
