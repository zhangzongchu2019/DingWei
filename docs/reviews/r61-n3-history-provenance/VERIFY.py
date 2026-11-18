#!/usr/bin/env python3
"""离线核Git对象、来源及作者证据；不联网、不运行Hub/负载。"""
from pathlib import Path
import subprocess,json,hashlib,gzip
ROOT=Path(__file__).resolve().parents[3]
DOC=Path(__file__).resolve().parent
BASE='206d41b26dab646c0e50a111eed5a45ccf7c72f6'
DESIGN='6a60a1a625ba36e8240866d641050ed83aaa64ef'
FIRST='c3b3db27c9532ab19a80d769da55b98f02e08696'
SOURCE='a32c877cf6ef21a909d2806f4b08e3e925d05d98'
def git(*args):return subprocess.check_output(['git','-C',str(ROOT),*args])
def digest(raw):return hashlib.sha256(raw).hexdigest()
def main():
 git('merge-base','--is-ancestor',BASE,'HEAD')
 assert not git('diff',BASE,SOURCE,'--','internal/store/migrations'), 'application migrations changed'
 assert not git('diff',DESIGN,FIRST,'--','openspec/changes/history-provenance')
 first=git('diff','--name-only',FIRST+'^',FIRST).decode().splitlines()
 assert len(first)==7 and all(p.startswith('openspec/changes/history-provenance/') for p in first)
 changes=git('diff','--name-only',SOURCE,'HEAD').decode().splitlines()
 assert all(p.startswith(('docs/','openspec/')) for p in changes),changes
 m=json.loads((DOC/'MANIFEST.json').read_text())
 assert m['source_commit']==SOURCE
 for p,h in m['files'].items():
  assert digest((ROOT/p).read_bytes())==h,p
  assert digest(git('cat-file','blob','HEAD:'+p))==h, 'Git blob '+p
 for p,h in m['source_files'].items():
  assert digest(git('cat-file','blob',SOURCE+':'+p))==h,p
  assert digest(git('cat-file','blob','HEAD:'+p))==h,p
  assert digest((ROOT/p).read_bytes())==h,p
 for raw in json.loads((DOC/'evidence/raw-logs.json').read_text()):
  if 'archive_path' in raw:
   b=(ROOT/raw['archive_path']).read_bytes()
   assert digest(b)==raw['archive_sha256']
   decoded=gzip.decompress(b)
   assert digest(decoded)==raw['sha256'] and len(decoded)==raw['bytes']
 gates=json.loads((DOC/'evidence/gates.json').read_text())
 assert {g['name'] for g in gates}=={'build-canonical','store-full3','m8-full3','focused-race','browser-full','strict'}
 assert all(g['rc']==0 and g['source_commit']==SOURCE for g in gates)
 fixture=json.loads((DOC/'evidence/fixture-export.json').read_text())
 assert fixture['source_commit']==SOURCE and fixture['rc']==0
 replay=json.loads((DOC/'evidence/history-fixture.json').read_text())
 assert set(replay)=={'agent','timer'}
 for v in replay.values():
  assert len(v['history']['events'])==1 and v['db_event']['provenance']['attested_by']=='hub'
  assert v['delivery_id']==v['origin_delivery_id'] and v['turn_id'] and 'id: ' in v['sse']
 cuts=json.loads((DOC/'evidence/mutations/summary.json').read_text())
 assert cuts['source']==SOURCE and cuts['valid']==cuts['killed']==40 and cuts['clean_after']
 for r in cuts['results']:
  assert digest((DOC/'evidence/mutations'/(r['name']+'.log')).read_bytes())==r['raw_sha256']
  if 'killed' in r:assert r['killed'] and r['business_failure'] and not r['compile_error'] and not r['panic']
  else:assert r['rc']==0
 q=json.loads((DOC/'evidence/ruling-dee06b54/qualification.json').read_text())
 assert q['source_commit']==SOURCE and q['planned_candidate_rounds']==5
 if q['status']=='PASS':
  candidates=[r for r in q['results'] if r['name'].startswith('candidate-')]
  assert len(candidates)==5 and sum(r['disposition']=='GREEN' for r in candidates)>=3
  assert all(r['disposition'] in ('GREEN','KNOWN_N4_ONLY') for r in q['results'])
  assert any(r['name']=='main-comparison' for r in q['results'])
 print('VERIFY_EVIDENCE_OK qualification='+q['status']+' source='+SOURCE+' evidence_files='+str(len(m['files']))+' mutations40 product_unchanged first_design_exact')
if __name__=='__main__':main()
