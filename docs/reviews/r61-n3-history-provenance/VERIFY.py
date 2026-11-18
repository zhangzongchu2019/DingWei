#!/usr/bin/env python3
"""离线验证当前N-3源码与证据；不联网、不启动Hub或测试负载。"""
from pathlib import Path
import subprocess,json,hashlib,gzip
ROOT=Path(__file__).resolve().parents[3]
DOC=Path(__file__).resolve().parent
BASE='206d41b26dab646c0e50a111eed5a45ccf7c72f6'
DESIGN='6a60a1a625ba36e8240866d641050ed83aaa64ef'
FIRST='c3b3db27c9532ab19a80d769da55b98f02e08696'
SOURCE='9313197f8ac3e2a585cd399935d5b313084b3b1e'
PREVIOUS='a32c877cf6ef21a909d2806f4b08e3e925d05d98'
EVIDENCE=DOC/'evidence/legacy-reanchor-20261118'
def git(*args):return subprocess.check_output(['git','-C',str(ROOT),*args])
def digest(raw):return hashlib.sha256(raw).hexdigest()
def main():
 git('merge-base','--is-ancestor',BASE,'HEAD')
 git('merge-base','--is-ancestor',PREVIOUS,SOURCE)
 assert not git('diff',BASE,SOURCE,'--','internal/store/migrations')
 assert not git('diff',PREVIOUS,SOURCE,'--','internal/store/history_migrations')
 assert not git('diff',DESIGN,FIRST,'--','openspec/changes/history-provenance')
 first=git('diff','--name-only',FIRST+'^',FIRST).decode().splitlines()
 assert len(first)==7 and all(p.startswith('openspec/changes/history-provenance/') for p in first)
 changes=git('diff','--name-only',SOURCE,'HEAD').decode().splitlines()
 assert all(p.startswith(('docs/','openspec/')) for p in changes),changes
 m=json.loads((DOC/'MANIFEST.json').read_text());assert m['source_commit']==SOURCE
 for p,h in m['files'].items():
  assert digest((ROOT/p).read_bytes())==h,p
  assert digest(git('cat-file','blob','HEAD:'+p))==h,'Git blob '+p
 for p,h in m['source_files'].items():
  assert digest(git('cat-file','blob',SOURCE+':'+p))==h,p
  assert digest(git('cat-file','blob','HEAD:'+p))==h,p
  assert digest((ROOT/p).read_bytes())==h,p
 # 原A32历史原始归档不删除、不覆写；新增日志明确区分原始与脱敏发布SHA。
 for record in json.loads((DOC/'evidence/raw-logs.json').read_text()):
  if 'archive_path' in record:
   archive=(ROOT/record['archive_path']).read_bytes();assert digest(archive)==record['archive_sha256']
   decoded=gzip.decompress(archive);assert digest(decoded)==record['sha256'] and len(decoded)==record['bytes']
 for record in json.loads((EVIDENCE/'raw-logs.json').read_text()):
  if 'archive_path' in record:
   archive=(ROOT/record['archive_path']).read_bytes();assert digest(archive)==record['archive_sha256'];decoded=gzip.decompress(archive)
  else:decoded=(ROOT/record['published_path']).read_bytes()
  assert digest(decoded)==record['published_sha256'] and len(decoded)==record['published_bytes']
 gates=json.loads((EVIDENCE/'gates.json').read_text())
 assert {r['name'] for r in gates}=={'new-mutations','all40-mutations','build-canonical','store-full3','m8-full3','focused-race','browser-full','strict'}
 assert all(r['rc']==0 and r['source_commit']==SOURCE for r in gates)
 fixture=json.loads((EVIDENCE/'fixture-export.json').read_text());assert fixture['source_commit']==SOURCE and fixture['rc']==0
 replay=json.loads((EVIDENCE/'history-fixture.json').read_text());assert set(replay)=={'agent','timer'}
 for row in replay.values():
  assert len(row['history']['events'])==1 and row['db_event']['provenance']['attested_by']=='hub'
  assert row['delivery_id']==row['origin_delivery_id'] and row['turn_id'] and 'id: ' in row['sse']
 cuts=json.loads((EVIDENCE/'mutations/summary.json').read_text())
 assert cuts['source']==SOURCE and cuts['valid']==cuts['killed']==40 and cuts['clean_after']
 for row in cuts['results']:
  assert digest((EVIDENCE/'mutations'/(row['name']+'.log')).read_bytes())==row['raw_sha256']
  if 'killed' in row:assert row['killed'] and row['business_failure'] and not row['compile_error'] and not row['panic']
  else:assert row['rc']==0
 new=json.loads((EVIDENCE/'new-mutations.json').read_text());assert len(new)==2
 for row in new:
  assert row['source']==SOURCE and row['build_rc']==0 and row['test_rc']!=0 and row['business_assertion']
  assert row['original_sha256']==row['restored_sha256']==digest(git('cat-file','blob',SOURCE+':internal/m8/hub.go'))
  for phase in ['build','test']:assert digest((EVIDENCE/(row['name']+'-'+phase+'.log')).read_bytes())==row[phase+'_raw_sha256']
 controls=json.loads((EVIDENCE/'current-route-controls.json').read_text());assert len(controls)==2 and all(r['rc']==0 for r in controls) and {r['source_commit'] for r in controls}=={SOURCE,PREVIOUS}
 old=json.loads((EVIDENCE/'old-product-red.json').read_text());assert old['rc']!=0 and old['source_product']==PREVIOUS
 print('VERIFY_OK source='+SOURCE+' evidence_files='+str(len(m['files']))+' mutations42 old40rerun_plus_new2 product_unchanged first_design_exact')
if __name__=='__main__':main()
