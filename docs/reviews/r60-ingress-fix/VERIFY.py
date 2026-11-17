"""只读校验本次STOP证据；不运行产品、不访问网络，不把STOP当最终PASS。"""
from pathlib import Path
import gzip,hashlib,json,subprocess
HERE=Path(__file__).resolve().parent
ROOT=Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],cwd=HERE,text=True).strip())
SOURCE='a24bb16335040ae06486f1c76be69ee542958214'
FAILED='TestSessionNameReconnectKeepsNameAndReleaseReuses'
def check(ok,why):
 if not ok:raise SystemExit('VERIFY_FAILED '+why)
def sha(b):return hashlib.sha256(b).hexdigest()
def git(*args):return subprocess.check_output(['git',*args],cwd=ROOT)
def doc(p):return json.loads((HERE/p).read_text())
def raw(p):
 b=(HERE/p).read_bytes();return gzip.decompress(b) if str(p).endswith('.gz') else b
manifest=doc('stopped/product-manifest.json');check(manifest['source_commit']==SOURCE,'source anchor')
check(git('show','-s','--format=%T',SOURCE).decode().strip()==manifest['tree'],'source tree')
check(git('show','-s','--format=%P',SOURCE).decode().strip()==manifest['parent'],'source parent')
for row in manifest['files']:
 check(sha(git('show',SOURCE+':'+row['path']))==row['sha256'],'source '+row['path'])
 check(sha((ROOT/row['path']).read_bytes())==row['sha256'],'current product '+row['path'])
check(not git('diff','--name-only',SOURCE,'--','internal','cmd','tools','scripts','deploy','view-service','go.mod','go.sum','VERSION').strip(),'current product differs from candidate')
gates=doc('stopped/gates/progress.json');by={r['name']:r for r in gates}
check(set(by)=={'build','m8-1','m8-2','m8-3','m8-4'},'extra or missing formal stages')
for row in gates:
 check(row['source_commit']==SOURCE and row['uid']!=0,'source/user '+row['name'])
 check(sha(raw('stopped/gates/'+row['name']+'.log.gz'))==row['raw_sha256'],'raw '+row['name'])
 check(row['env']['HUB_TEST_PG_STRICT']=='1','PG strict '+row['name'])
 check(row['env'].get('DW_DESKTOP_INTEGRATION_PYTHON'),'isolated Python '+row['name'])
 check(not any(k.startswith(('SH_','DINGWEI_','DATAD_')) for k in row['env']),'inherited environment')
check(by['build']['rc']==0,'complete same-source canonical rerun')
for n in range(1,4):
 row=by['m8-'+str(n)];check(row['rc']==0 and not row['failed_tests'] and not row['interrupted'],'completed green round'+str(n))
row=by['m8-4'];check(row['rc']==1 and row['failed_tests']==[FAILED],'fourth round unique failure')
check(len(row['interrupted'])==1 and row['interrupted'][0]['test']==FAILED and 'hub_test.go:3505: interrupted (9)'in row['interrupted'][0]['output'],'fourth raw failure')
stop=doc('stopped/gates/STOP.json');check(stop['invalid_failures']==[FAILED] and stop['stage']=='m8-4','STOP rule')
first=doc('stopped/attempts/canonical-first/build.json');check(first['source_commit']==SOURCE and first['rc']==1,'first canonical preserved')
check(sha(raw('stopped/attempts/canonical-first/build.log.gz'))==first['raw_sha256'],'first canonical raw')
for group in ['new-mutations','retained-mutations']:
 rows=doc('stopped/'+group+'/summary.json');check(len(rows)==28,group+' count')
 for row in rows:
  check(row['source_commit']==SOURCE and row['build_rc']==0 and row['rc'] not in (None,0) and row['failed_assertions'] and row['killed'],group+' business knife '+row['name'])
  check(sha(raw('stopped/'+group+'/'+row['name']+'.log.gz'))==row['raw_sha256'],group+' raw '+row['name'])
for row in doc('stopped/restored-baselines/summary.json'):
 check(row['source_commit']==SOURCE and row['rc']==0,'restored baseline')
 check(sha(raw('stopped/restored-baselines/'+row['name']+'.log.gz'))==row['raw_sha256'],'restored raw')
cli=doc('stopped/named-cli-within-four-whole-rounds.json')
for r in cli['rounds']:
 check(len(r['outcomes'])==2 and all(e['Action']=='pass'for e in r['outcomes']),'named CLI outcomes inside round'+str(r['round']))
summary=doc('stopped/summary.json');check(summary['m8_round5_not_run'] and summary['failure_test']==FAILED,'stop summary')
# Non-Z3 gates are a separate append-only continuation under the 20:01Z ruling.
# They never authorize a fifth whole-m8 sample or turn STOP into full PASS.
if (HERE/'non-z3').exists():
 first_browser=doc('non-z3-browser-first/browser.json')
 check(first_browser['source_commit']==SOURCE and first_browser['rc']==2,'first browser failure preserved')
 check(sha(raw('non-z3-browser-first/browser.log.gz'))==first_browser['raw_sha256'],'first browser raw')
 first_receipt=(HERE/'non-z3-browser-first/browser-receipt.json').read_text()
 check('suite.mcp_zero_consent.rc=3' in first_receipt and 'aggregate_rc=2' in first_receipt,'first browser environment failure')
 complete=doc('non-z3/COMPLETE-NON-Z3.json')
 check(complete['source_commit']==SOURCE and 'STOP' in complete['Z3'],'non-Z3 authority boundary')
 rows=doc('non-z3/progress.json'); by_non={r['name']:r for r in rows}
 check(set(by_non)=={'go-other-trees','focused-race','joint-and-dual-cli','browser','strict'},'non-Z3 stages')
 for r in rows:
  check(r['source_commit']==SOURCE and r['uid']!=0 and r['rc']==0,'non-Z3 source/user/result '+r['name'])
  check(sha(raw('non-z3/'+r['name']+'.log.gz'))==r['raw_sha256'],'non-Z3 raw '+r['name'])
  check(r['env']['HUB_TEST_PG_STRICT']=='1' and r['env'].get('DW_DESKTOP_INTEGRATION_PYTHON'),'non-Z3 explicit environment')
  check(not any(k.startswith(('SH_','DINGWEI_','DATAD_')) for k in r['env']),'non-Z3 inherited environment')
 check(not by_non['focused-race']['data_race'] and not by_non['focused-race']['skipped_tests'],'focused race')
 r=by_non['joint-and-dual-cli'];check(r['top_pass']==2 and not r['skipped_tests'],'named gates really ran')
 receipt=(HERE/'non-z3/browser-receipt.json').read_text()
 suites=[l for l in receipt.splitlines() if l.startswith('suite.')]
 check('commit_sha='+SOURCE in receipt and 'aggregate_rc=0' in receipt and len(suites)==45 and all(l.endswith('.rc=0') for l in suites),'full browser receipt')
 for r in doc('non-z3/integration-cuts/summary.json'):
  check(r['source_commit']==SOURCE and r['build_rc']==0 and r['rc']!=0 and r['killed'] and r['failed_assertions'],'integration guard knife')
  check(sha(raw('non-z3/integration-cuts/'+r['name']+'.log.gz'))==r['raw_sha256'],'integration guard raw')
 check(len(doc('non-z3/integration-cuts/summary.json'))==2,'two integration guard cuts')
 for r in doc('non-z3/integration-cuts/baselines.json'):
  check(r['source_commit']==SOURCE and r['rc']==0,'integration restored baseline')
  check(sha(raw('non-z3/integration-cuts/'+r['name']+'.log.gz'))==r['raw_sha256'],'integration restored raw')
 print('VERIFY_OK_NON_Z3 gates5 integration_cuts2; historical_STOP_boundary_preserved')
# The historical stop above remains verified, but the leader authorized a fresh gate.
revised=HERE/'z3-revised-five'
if revised.exists():
 policy=doc('z3-revised-five/POLICY.json');complete=doc('z3-revised-five/COMPLETE.json')
 expected=['TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses']
 check(policy['authority_utc']=='2026-11-17T21:00:33Z' and policy['allowed']==expected,'leader revised gate list')
 check(complete['source_commit']==SOURCE and complete['whole_rounds']==5 and complete['prior_four_reused'] is False,'fresh five completion')
 rows=doc('z3-revised-five/progress.json')
 check([r['name'] for r in rows]==['m8-'+str(i) for i in range(1,6)],'exactly five fresh serial rounds')
 previous=policy['authority_utc'];green=0
 for i,r in enumerate(rows,1):
  check(r['source_commit']==SOURCE and r['uid']!=0,'fresh source/user')
  check(r['utc_start']>=previous and r['utc_end']>=r['utc_start'],'serial chronology')
  previous=r['utc_end']
  b=raw('z3-revised-five/'+r['name']+'.log.gz');check(sha(b)==r['raw_sha256'],'fresh raw '+r['name'])
  check(r['env']['HUB_TEST_PG_STRICT']=='1' and r['env'].get('DW_DESKTOP_INTEGRATION_PYTHON'),'fresh PG/CLI environment')
  check(not any(k.startswith(('SH_','DINGWEI_','DATAD_')) for k in r['env']),'fresh inherited environment')
  import re
  events=[]
  for l in b.decode().splitlines():
   try:e=json.loads(l)
   except ValueError:continue
   if isinstance(e,dict) and 'Action' in e:events.append(e)
  failures=[e['Test'] for e in events if e.get('Action')=='fail' and e.get('Test')]
  leaf=[t for t in failures if not any(o.startswith(t+'/') for o in failures)]
  check(failures==r['failed_tests'],'fresh raw failure tally')
  check(sum(e.get('Action')=='pass' and bool(e.get('Test')) for e in events)==r['all_pass'],'fresh raw PASS tally')
  check([e['Test'] for e in events if e.get('Action')=='skip' and e.get('Test')]==r['skipped_tests'],'fresh raw SKIP tally')
  for t in leaf:
   check(t in expected,'fresh failure outside authorized list')
   output=''.join(e.get('Output','') for e in events if e.get('Test')==t)
   diagnostics=[l for l in output.splitlines() if re.search(r'\b[\w./-]+_test\.go:\d+:',l)]
   check(diagnostics and all('interrupted (9)' in l for l in diagnostics),'fresh failure is not solely interrupted')
  for e in r['interrupted']:check(e['test'] in expected,'fresh interrupted outside context')
  assessment=doc('z3-revised-five/'+r['name']+'-assessment.json');check(not assessment['stop_reasons'],'fresh assessed STOP')
  check(r['rc'] in (0,1) and (r['rc']==0 or failures),'fresh package/process failure')
  if r['rc']==0 and not failures:green+=1
 check(green>=3 and green==complete['whole_green'],'at least three fully green rounds')
 r=doc('z3-revised-five/final-docs-strict.json');check(r['source_commit']==SOURCE and r['rc']==0,'final documentation strict')
 check(sha(raw('z3-revised-five/final-docs-strict.log.gz'))==r['raw_sha256'],'final strict raw')
 reference=doc('z3-revised-five/causation-reference.json')
 check(reference['commit']=='43ec0a67f0ef7862139d4831b69c2a511a90b4ee','causation reference anchor')
 check(sha(git('show',reference['commit']+':'+reference['report_path']))==reference['report_sha256'],'causation Git report')
 for r in reference['raw_manifest_verified']:check(sha(git('show',reference['commit']+':'+r['path']))==r['sha256'],'causation reference raw')
 print('VERIFY_OK_REVISED_Z3 source',SOURCE,'fresh_rounds5 whole_green',green,'historical_four_retained; AUTHOR_GATES_COMPLETE_REVIEW_REQUIRED_NO_GO')
else:
 print('VERIFY_OK_STOP source',SOURCE,'products',len(manifest['files']),'new_knives28 retained_replays28 whole_m8_rounds4 fourth_outside_allowlist; NOT_FULL_PASS')
