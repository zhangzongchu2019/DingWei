"""一次性副本语义撤防；绿控制、每刀指定判据与原文必须可核验。"""
from pathlib import Path
import argparse
import datetime as dt
import json
import os
import shutil
import subprocess
import sys
import tempfile

ROOT=Path(__file__).resolve().parents[3]
REC='test_codex_stale_thread.RecoveryThreadInheritance.'
LANE='test_sub_agent_helper.RecoveryRootLaneTests.'
CASES=[
 ('inherit','driver_adapter.py','                    inherit(old)','                    pass',REC+'test_adapter_recovery_inherits_final_thread_and_never_replays_unknown','HRI001_INHERIT_FINAL_ROOT'),
 ('fallback','codexDriver.py','if (not reason or not self._recovery_thread_id','if (True or not self._recovery_thread_id',REC+'test_missing_or_wrong_cwd_warns_and_emits_once_before_new_turn','HRI002_FALLBACK_NOT_ADMITTED'),
 ('warning','codexDriver.py',"""            print(f'[delivery][THREAD_REPLACED] old_thread={replacing_thread_id} '
                  f'new_thread={self._thread_id} reason={replacing_reason}', flush=True)""",'            pass',REC+'test_missing_or_wrong_cwd_warns_and_emits_once_before_new_turn','HRI002_FIXED_WARNING_MISSING'),
 ('event','codexDriver.py',"'change': 'delivery_thread_replaced'","'change': 'disabled_thread_event'",REC+'test_missing_or_wrong_cwd_warns_and_emits_once_before_new_turn','HRI002_STATE_EVENT_MISSING'),
 ('explicit-bound','codexDriver.py','self._required_bound_thread_id = bound_thread_id if require_bound_thread else ""','self._required_bound_thread_id = ""',REC+'test_configured_root_and_child_still_refuse_unavailable_thread','HRI002_BOUND_OR_CHILD_OPENED'),
 ('dynamic-root','sub_agent_runtime.py',"lambda: self._driver_for('', 1, self.root_adapter.driver._thread_id)","lambda: self._driver_for('', 1, driver._thread_id)",LANE+'test_recovered_root_rotates_without_rebinding_children_and_factory_tracks_latest','HRI004_ROOT_FACTORY_CAPTURED_FIRST_ID'),
 ('no-replay','codexDriver.py','if existing is not None and (existing.state != "failed" or existing.turn_id','if False and (existing.state != "failed" or existing.turn_id',REC+'test_adapter_recovery_inherits_final_thread_and_never_replays_unknown',"replacement.deliver('uncertain', 'original').state, 'failed'"),
 ('cross-lane-write','app_server_broker.py',"not params['threadId'] or params['threadId'] not in allowed","not params['threadId']",LANE+'test_recovered_root_rotates_without_rebinding_children_and_factory_tracks_latest',"child.request('turn/start',dict(threadId=latest,input=[]))"),
 ('private-hint','private_endpoint.py',"'boot': current['boot'], 'thread_id': thread_id})\n\n    def resume_command","'boot': current['boot'], 'thread_id': previous})\n\n    def resume_command",LANE+'test_private_replacement_requires_writer_and_exact_previous_binding','HRI004_PRIVATE_HINT_NOT_REPLACED'),
]

def main():
 parser=argparse.ArgumentParser();parser.add_argument('--output',required=True,type=Path);args=parser.parse_args()
 args.output.mkdir(parents=True,exist_ok=True);results=[]
 with tempfile.TemporaryDirectory(prefix='hri-mut-',dir='/tmp') as temp:
  temp=Path(temp)
  for name in ('sessionhelper','tests'):
   shutil.copytree(ROOT/'tools/sessionhelper'/name,temp/name,ignore=shutil.ignore_patterns('__pycache__','*.pyc'))
  (temp/'home').mkdir();(temp/'tmp').mkdir()
  env=dict(HOME=str(temp/'home'),TMPDIR=str(temp/'tmp'),PATH=os.environ['PATH'],LANG='C.UTF-8',TZ='UTC',PYTHONDONTWRITEBYTECODE='1',PYTHONPATH=str(temp)+':'+str(temp/'tests'))
  def run(name,selectors):
   with (args.output/(name+'.log')).open('w') as out:
    return subprocess.run([sys.executable,'-B','-m','unittest','-v',*selectors],cwd=temp,env=env,stdout=out,stderr=subprocess.STDOUT,timeout=60).returncode
  control=run('control',list(dict.fromkeys(c[4] for c in CASES)))
  if control:raise SystemExit('green control failed rc='+str(control))
  for name,file,before,after,selector,criterion in CASES:
   p=temp/'sessionhelper'/file;original=p.read_text()
   assert original.count(before)==1,(name,original.count(before))
   try:
    p.write_text(original.replace(before,after,1));rc=run(name,[selector]);log=(args.output/(name+'.log')).read_text()
    killed=rc==1 and 'FAIL:' in log and criterion in log
    results.append(dict(name=name,test=selector,criterion=criterion,rc=rc,killed=killed))
   finally:p.write_text(original)
 summary=dict(utc=dt.datetime.now(dt.timezone.utc).isoformat(),control_rc=control,total=len(results),killed=sum(x['killed'] for x in results),results=results)
 (args.output/'mutations.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps(summary,ensure_ascii=False));return int(not all(x['killed'] for x in results))
if __name__=='__main__':raise SystemExit(main())
