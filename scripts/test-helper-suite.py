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


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--driver',choices=('all','unset','claude','codex'),default='all')
    parser.add_argument('--pattern',default='test_*.py')
    args=parser.parse_args()
    root=Path(__file__).resolve().parents[1]
    environment={key:value for key,value in os.environ.items()
                 if not key.startswith(('SH_','DW_','DINGWEI_','DATAD_'))}
    environment['PYTHONPATH']=str(root/'tools/sessionhelper')+os.pathsep+str(root/'view-service')
    if args.driver=='all':
        results=[subprocess.run([sys.executable,str(Path(__file__).resolve()),'--driver',profile,'--pattern',args.pattern],cwd=root,env=environment).returncode for profile in ('claude','codex')]
        return 1 if any(results) else 0
    if args.driver!='unset':environment['SH_DRIVER']=args.driver
    print(f'HELPER_SUITE python={sys.executable} version={sys.version.split()[0]} driver={args.driver} pattern={args.pattern}',flush=True)
    return subprocess.run([sys.executable,'-m','unittest','discover','-s','tools/sessionhelper/tests','-p',args.pattern,'-v'],cwd=root,env=environment).returncode


if __name__=='__main__':raise SystemExit(main())
