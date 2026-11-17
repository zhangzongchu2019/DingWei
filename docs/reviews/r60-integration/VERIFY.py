#!/usr/bin/env python3
"""在仓根运行 python3 docs/reviews/r60-integration/VERIFY.py。只读，不跑模型/现网。"""
import hashlib,json,re,subprocess
from pathlib import Path
root=Path(__file__).resolve().parents[3];d=root/'docs/reviews/r60-integration';e=d/'evidence'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def git(*a):return subprocess.check_output(['git',*a],cwd=root)
g=json.loads((e/'gate-summary.json').read_text());s=g['source_commit'];assert g['gate']=='PASS'
m=json.loads((e/'product-manifest.json').read_text());assert m['source_commit']==s
for v in m['files']:
 blob=git('show',s+':'+v['path']);assert hashlib.sha256(blob).hexdigest()==v['sha256'];assert sha(root/v['path'])==v['sha256']
for meta in e.glob('*.json'):
 try:v=json.loads(meta.read_text())
 except (ValueError,UnicodeError):continue
 if isinstance(v,dict) and 'raw_sha256' in v:
  log=meta.with_suffix('.log');assert log.exists() and sha(log)==v['raw_sha256'],meta.name
  assert v.get('source_commit') in (None,s),meta.name
  if v.get('source_commit')==s:assert not v.get('dirty_before') and not v.get('dirty_after') and v['head_after']==s
cuts=json.loads((e/'mutations.json').read_text());assert len(cuts)==8
for v in cuts:
 assert v['compile']['rc']==0 and v['result']['rc']!=0 and v['failed_assertions']
 for key in ('compile','result'):
  t=v[key];path=e/('cut-'+v['name']+('-compile' if key=='compile' else '')+'.log');assert sha(path)==t['raw_sha256']
 assert v['before']!=v['after']
 assert v['before'] in git('show',s+':'+v['file']).decode()
assert json.loads((e/'mutations-restored.json').read_text())['rc']==0
assert json.loads((e/'heartbeat-restored.json').read_text())['rc']==0
allow={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to'}
assert len(g['runs'])==5
for v in g['runs']:
 log=e/(v['name']+'.log');assert sha(log)==v['raw_sha256']
 events=[json.loads(ln) for ln in log.read_text().splitlines() if ln.startswith('{')]
 interrupts=[x for x in events if re.search(r'interrupted\s*\(9\)|SQLITE_INTERRUPT',x.get('Output',''),re.I)]
 assert len(interrupts)==v['interrupted_lines']
 assert all(x.get('Test') in allow for x in interrupts)
 assert sum(x.get('Action')=='pass' and bool(x.get('Test')) for x in events)==v['all_pass']
 assert sum(x.get('Action')=='skip' and bool(x.get('Test')) for x in events)==v['all_skip']
 assert not v['dirty_before'] and not v['dirty_after'] and v['source_commit']==s
assert sum(x['rc']==0 and not x['failures'] for x in g['runs'])>=3
for key in ('go-other-four-trees','canonical-build','focused-race','browser-all','strict-all'):
 v=g[key];assert v['rc']==0 and v['source_commit']==s and not v['dirty_after'];assert sha(e/(key+'.log'))==v['raw_sha256']
assert 'BUILD_TESTS_OK helper_profiles=2 python=27 shell=2' in (e/'canonical-build.log').read_text()
assert 'DATA RACE' not in (e/'focused-race.log').read_text()
b=(e/'browser-all.log').read_text();assert 'suite.renderer_agent_icon_admission.rc=0' in b
assert 'retainCanonicalTrustedProvenance' in (root/'view-service/renderer-canary-sse.html').read_text()
assert (root/'VERSION').read_text().strip()=='0.2.3.40'
print('VERIFY_OK',s,'products',len(m['files']),'cuts',len(cuts),'m8_runs',len(g['runs']),'whole_green',sum(x['rc']==0 for x in g['runs']))
