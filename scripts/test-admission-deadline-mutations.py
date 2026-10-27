#!/usr/bin/env python3
"""Behavioral negative controls in disposable Git worktrees, no live helper."""
import argparse
from pathlib import Path
import os
import subprocess
import sys
import tempfile

ROOT=Path(__file__).resolve().parents[1]
SOURCE='tools/sessionhelper/sessionhelper/driver_adapter.py'
MUTANTS={
 'reuse-terminal-deadline':(
  "if not admitted and time.monotonic() - self._dispatch_clock(command) >= getattr(self, '_admission_timeout', 60.0):",
  'if not admitted and time.monotonic() - self._dispatch_clock(command) >= self._terminal_timeout:',3,
  'test_missing_accepted_uses_admission_deadline_and_recovers','独立admission截止必须越过阻塞deliver'),
 'sync-deliver':(
  'receipt = self._bounded_deliver(command)','receipt = self.driver.deliver(command.delivery_id, command.text)',2,
  'test_blocked_deliver_is_aborted_before_next_dispatch','独立admission截止必须越过阻塞deliver'),
 'restart-admission-clock':(
  "intent = row['dispatch_intent_at'] if row is not None else None",'intent = None',1,
  'test_clock_includes_time_since_persisted_dispatch_intent','admission截止须自intent落盘起计'),
 'accepted-reopens-unknown':(
  "if row['state'] != 'processing':return False\n            now=time.time()\n            if now >= row['dispatch_intent_at'] + timeout_seconds:",
  "if False:return False\n            now=time.time()\n            if False:",1,
  'test_slow_poll_accepted_after_deadline_is_not_admission','截止后accepted-only不得写injected或造成ACK矛盾'),
 'invent-native-times':(
  "admitted_at=data.get('admitted_at')\n        processed_at=data.get('processed_at')",
  "admitted_at=data.get('admitted_at',__import__('time').time())\n        processed_at=data.get('processed_at',__import__('time').time())",1,
  'test_unknown_terminal_without_original_times_is_evidence_only','缺原始双时刻的终态不得补造完整证据'),
}

def main():
 parser=argparse.ArgumentParser(description=__doc__)
 parser.add_argument('--ref',default='HEAD');parser.add_argument('--logs',required=True,type=Path)
 args=parser.parse_args();args.logs.mkdir(parents=True,exist_ok=True)
 for name,(old,new,count,test,criterion) in MUTANTS.items():
  with tempfile.TemporaryDirectory(prefix='r40-admission-mutation-') as parent:
   tree=Path(parent)/'tree'
   subprocess.run(['git','-C',str(ROOT),'worktree','add','--detach',str(tree),args.ref],check=True,stdout=subprocess.DEVNULL)
   try:
    source=tree/('tools/sessionhelper/sessionhelper/inbox_lifecycle.py' if name=='accepted-reopens-unknown' else SOURCE);body=source.read_text()
    if body.count(old)!=count:raise RuntimeError('stale mutation: '+name)
    source.write_text(body.replace(old,new))
    env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DW_','DINGWEI_','DATAD_'))}
    env['PYTHONPATH']=str(tree/'tools/sessionhelper')+os.pathsep+str(tree/'tools/sessionhelper/tests')
    with (args.logs/(name+'.log')).open('w') as log:
     result=subprocess.run([sys.executable,'-m','unittest','test_dispatch_lifecycle.'+('AdmissionDeadlineRaceTest.' if name in ('accepted-reopens-unknown','invent-native-times') else 'AdmissionDeadlineTest.')+test,'-v'],cwd=tree,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=20)
    matches=[line.strip() for line in (args.logs/(name+'.log')).read_text().splitlines() if criterion in line]
    if result.returncode==0 or not matches:raise RuntimeError(f'{name}: expected assertion RED at {criterion}; rc={result.returncode}')
    print(f'{name}: expected_red rc={result.returncode}: {matches[-1]}',flush=True)
   finally:subprocess.run(['git','-C',str(ROOT),'worktree','remove','--force',str(tree)],check=True,stdout=subprocess.DEVNULL)
if __name__=='__main__':main()
