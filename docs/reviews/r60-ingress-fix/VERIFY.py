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
 print('VERIFY_OK_NON_Z3 gates5 integration_cuts2; Z3_REMAINS_STOPPED')
print('VERIFY_OK_STOP source',SOURCE,'products',len(manifest['files']),'new_knives28 retained_replays28 whole_m8_rounds4 fourth_outside_allowlist; NOT_FULL_PASS')
