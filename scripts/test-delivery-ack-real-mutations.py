#!/usr/bin/env python3
"""Negative controls against real Hub/helper/native CLI, in disposable Git trees.

Requires the same DW_ACK_V2_E2E_PYTHON, NODE_PATH and installed CLI/browser as
TestDeliveryACKActualCLI*. Never edits the caller's tree or live helper state.
"""
import argparse
from pathlib import Path
import os
import subprocess
import tempfile

ROOT=Path(__file__).resolve().parents[1]
P='tools/sessionhelper/sessionhelper/driver_adapter.py'
MUTANTS={
 'enqueue-as-admitted':(
  '        self._publish_queue_event(result.event)\n        if result.state == "rejected":',
  '''        if result.state == "queued" and self._commands.current is not None:
            ledger = self.delivery_lifecycle
            ledger.begin_dispatch(result.command.delivery_id)
            ledger.record_admitted(result.command.delivery_id, "enqueue-only")
        self._publish_queue_event(result.event)
        if result.state == "rejected":''',
  '^TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude$',
  '判据2：A长回合期间B排队不得提前 injected/processed'),
 'return-as-terminal':(
  '            ledger.record_admitted(command.delivery_id, turn_id)',
  '''            ledger.record_admitted(command.delivery_id, turn_id)
            # Keep the first settled command as the native setup barrier. The
            # negative control removes terminal association for subsequent work,
            # without hardcoding any delivery ID or weakening criterion 4.
            if any(prior.state in ('completed', 'interrupted') for prior in self._commands._by_delivery.values()):
                ledger.record_completed(command.delivery_id, turn_id, 'completed')''',
  '^TestDeliveryACKActualCLIQueuedAdmissionAndTerminal/claude$',
  '判据4：B真实终态前不得因函数返回标 processed'),
 'drop-recovery-reconciliation':(
  '        for row in ledger.recoverable_lifecycles():',
  '        for row in []:',
  '^TestDeliveryACKActualCLIAdmissionCrashWindows/claude/dispatch_sent_receipt_unread$',
  '判据8：等待原请求恢复对账实际完成'),
}

def main():
 parser=argparse.ArgumentParser(description=__doc__)
 parser.add_argument('--ref',default='HEAD')
 parser.add_argument('--logs',required=True,type=Path)
 parser.add_argument('--mutant',choices=MUTANTS)
 args=parser.parse_args();args.logs.mkdir(parents=True,exist_ok=True)
 if not os.environ.get('DW_ACK_V2_E2E_PYTHON'):raise SystemExit('INCONCLUSIVE: real CLI Python unset')
 selected=[args.mutant] if args.mutant else list(MUTANTS)
 for name in selected:
  old,new,test,criterion=MUTANTS[name]
  with tempfile.TemporaryDirectory(prefix='r40-ack-mutant-') as parent:
   tree=Path(parent)/'tree'
   subprocess.run(['git','-C',str(ROOT),'worktree','add','--detach',str(tree),args.ref],check=True,stdout=subprocess.DEVNULL)
   try:
    source=tree/P;body=source.read_text()
    if body.count(old)!=1:raise RuntimeError('stale mutation probe: '+name)
    source.write_text(body.replace(old,new))
    log=args.logs/(name+'.log')
    with log.open('w') as output:
     result=subprocess.run(['go','test','-p','1','-count=1','-v','./internal/m8','-run',test],cwd=tree,stdout=output,stderr=subprocess.STDOUT)
    matches=[line.strip() for line in log.read_text().splitlines() if criterion in line]
    if result.returncode==0 or not matches:raise RuntimeError(f'{name}: expected behavioral RED at {criterion}; rc={result.returncode}; see {log}')
    print(f'{name}: expected_red rc={result.returncode}: {matches[-1]}',flush=True)
   finally:
    subprocess.run(['git','-C',str(ROOT),'worktree','remove','--force',str(tree)],check=True,stdout=subprocess.DEVNULL)
if __name__=='__main__':main()
