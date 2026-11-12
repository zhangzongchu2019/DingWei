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
REC='test_dispatch_lifecycle.BusyAdmissionTest.'
_adapter=(ROOT/'tools/sessionhelper/sessionhelper/driver_adapter.py').read_text()
_bounded=_adapter[_adapter.index('    def _bounded_deliver('):_adapter.index('    def _recover_transport(')]
CASES=[
 ('codex-wait-evidence','codexDriver.py','                self._busy_admission.waiting(envelope_id)','                pass',REC+'test_codex_busy90_once','BA_WAIT_NOT_OBSERVED'),
 ('claude-wait-evidence','claudeDriver.py','                self._busy_admission.waiting(envelope_id)','                pass',REC+'test_claude_busy90_once','BA_WAIT_NOT_OBSERVED'),
 ('codex-wait-limit','driver_adapter.py','if elapsed >= limit:','if False and elapsed >= limit:',REC+'test_codex_limit_cancels_late_submission','BA_LIMIT_NOT_ENFORCED'),
 ('claude-wait-limit','driver_adapter.py','if elapsed >= limit:','if False and elapsed >= limit:',REC+'test_claude_limit_cancels_late_submission','BA_LIMIT_NOT_ENFORCED'),
 ('warning','driver_adapter.py','[delivery][ALERT][BUSY_ADMISSION_TIMEOUT]','[delivery][DISABLED]',REC+'test_claude_limit_cancels_late_submission','BA_LIMIT_WARNING_MISSING'),
 ('wait-event','driver_adapter.py',"'change': 'delivery_waiting_agent_idle'","'change': 'disabled_wait_event'",REC+'test_codex_busy90_once','BA_WAIT_EVENT_MISSING'),
 ('codex-bad-transport','driver_adapter.py',_bounded,_bounded.replace('self._recover_transport(command)','pass'),REC+'test_codex_bad_transport_old_handling','BA_BAD_TRANSPORT_NOT_RECOVERED'),
 ('claude-bad-transport','driver_adapter.py',"or not driver.health() or (callable(required)","or False or (callable(required)",REC+'test_disconnected_transport_has_no_busy_exemption','BA_DISCONNECTED_EXTENDED'),
 ('possible-submission','driver_adapter.py','or submitted(command.delivery_id) is not None','or False',REC+'test_possible_submission_has_no_busy_exemption','BA_POSSIBLE_SUBMISSION_EXTENDED'),
 ('codex-stuck-write','driver_adapter.py','(callable(required) and required(self._admission_timeout))','(False and callable(required) and required(self._admission_timeout))',REC+'test_codex_stuck_writer_has_no_busy_exemption','BA_STUCK_WRITER_EXTENDED'),
 ('claude-stuck-write','driver_adapter.py','(callable(required) and required(self._admission_timeout))','(False and callable(required) and required(self._admission_timeout))',REC+'test_claude_stuck_writer_has_no_busy_exemption','BA_STUCK_WRITER_EXTENDED'),
 ('cancel-boundary','busy_admission.py',"if self._states.get(delivery_id) == 'cancelled':","if False and self._states.get(delivery_id) == 'cancelled':",REC+'test_cancelled_waiter_cannot_cross_submission_boundary','BA_CANCELLED_CROSSED_BOUNDARY'),
 ('cancel-possible','busy_admission.py',"if self._states.get(delivery_id) != 'waiting':","if False and self._states.get(delivery_id) != 'waiting':",REC+'test_submission_boundary_cannot_be_cancelled','BA_POSSIBLE_SUBMISSION_CANCELLED'),
 ('config','config.py','busy_admission_max_seconds=max(0.1, float(env.get("SH_BUSY_ADMISSION_MAX_SECONDS", "1800")))','busy_admission_max_seconds=1800.0',REC+'test_busy_configuration_default_and_override','BA_CONFIG_LIMIT_IGNORED'),
 ('view-wait-label','../view-service/renderer-canary-sse.html',"const stageText = busyWait ? '排队等待 Agent 空闲' : stageFooterText(norm);","const stageText = stageFooterText(norm);",'browser','BUSY_VISIBLE_WAITING'),
 ('view-prefix-merge','../view-service/renderer-canary-sse.html','if (existingKey) waitKey = existingKey;','if (false && existingKey) waitKey = existingKey;','browser','BUSY_MERGE_PREFIX_ONCE'),
 ('view-canonical-input','../view-service/renderer-canary-sse.html','else if (ev.text && !busyWait)','else if (ev.text)','browser','BUSY_CANONICAL_INPUT_PRESERVED'),
 ('view-busy-before-primary','../view-service/renderer-canary-sse.html',"} else if (ev.kind === 'input_posted') {\n        const placeholderKey","} else if (false && ev.kind === 'input_posted') {\n        const placeholderKey",'browser','BUSY_BEFORE_PRIMARY_MERGED'),
 ('view-late-no-requeue','../view-service/renderer-canary-sse.html','if (norm.key && c1InputCards.has(norm.key)) return false;','if (false && norm.key && c1InputCards.has(norm.key)) return false;','browser','BUSY_LATE_EVENT_NO_REQUEUE'),
]


def main():
 parser=argparse.ArgumentParser();parser.add_argument('--output',required=True,type=Path);args=parser.parse_args()
 args.output.mkdir(parents=True,exist_ok=True);results=[]
 with tempfile.TemporaryDirectory(prefix='busy-mut-',dir='/tmp') as temp:
  temp=Path(temp)
  for name in ('sessionhelper','tests'):
   shutil.copytree(ROOT/'tools/sessionhelper'/name,temp/name,ignore=shutil.ignore_patterns('__pycache__','*.pyc'))
  (temp/'view-service').mkdir()
  for name in ('renderer-canary-sse.html','renderer-model-admission-regression.js','renderer-regression-selfhost.js'):
   shutil.copy(ROOT/'view-service'/name,temp/'view-service'/name)
  (temp/'home').mkdir();(temp/'tmp').mkdir()
  env=dict(HOME=str(temp/'home'),TMPDIR=str(temp/'tmp'),PATH=os.environ['PATH'],LANG='C.UTF-8',TZ='UTC',PYTHONDONTWRITEBYTECODE='1',PYTHONPATH=str(temp)+':'+str(temp/'tests'))
  for key in ('NODE_PATH','PLAYWRIGHT_BROWSERS_PATH'):
   if key in os.environ:env[key]=os.environ[key]
  def run(name,selectors):
   with (args.output/(name+'.log')).open('w') as out:
    command=['node',str(temp/'view-service/renderer-model-admission-regression.js')] if selectors==['browser'] else [sys.executable,'-B','-m','unittest','-v',*selectors]
    return subprocess.run(command,cwd=temp,env=env,stdout=out,stderr=subprocess.STDOUT,timeout=60).returncode
  control=run('control',list(dict.fromkeys(c[4] for c in CASES if c[4]!='browser')))
  browser_control=run('browser-control',['browser'])
  if browser_control:raise SystemExit('browser green control failed rc='+str(browser_control))
  if control:raise SystemExit('green control failed rc='+str(control))
  for name,file,before,after,selector,criterion in CASES:
   p=temp/'sessionhelper'/file;original=p.read_text()
   assert original.count(before)==1,(name,original.count(before))
   try:
    p.write_text(original.replace(before,after,1));rc=run(name,[selector]);log=(args.output/(name+'.log')).read_text()
    killed=((rc==1 and 'FAIL:' in log) or (selector=='browser' and rc==2 and '  FAIL ' in log)) and criterion in log
    results.append(dict(name=name,test=selector,criterion=criterion,rc=rc,killed=killed))
   finally:p.write_text(original)
 summary=dict(utc=dt.datetime.now(dt.timezone.utc).isoformat(),control_rc=control,browser_control_rc=browser_control,total=len(results),killed=sum(x['killed'] for x in results),results=results)
 (args.output/'mutations.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps(summary,ensure_ascii=False));return int(not all(x['killed'] for x in results))
if __name__=='__main__':raise SystemExit(main())
