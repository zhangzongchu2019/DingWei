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
print('VERIFY_OK_STOP source',SOURCE,'products',len(manifest['files']),'new_knives28 retained_replays28 whole_m8_rounds4 fourth_outside_allowlist; NOT_FULL_PASS')
