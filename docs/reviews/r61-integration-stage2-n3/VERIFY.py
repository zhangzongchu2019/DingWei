#!/usr/bin/env python3
"""离线复核N1b＋N3合并、固定源等价及逐门证据；不启动服务。"""
from pathlib import Path
import subprocess,hashlib,json,gzip,re
ROOT=Path(__file__).resolve().parents[3];D=Path(__file__).resolve().parent
S='4581b34ac1e4b9973d12c559cf6ac7772107bebf';B='c430c3c65f0f8d9b669c8bef24f4194871f11283';N='1a2a4160208c839c97e1b35039c7eafc6e7216e1'
def git(*a):return subprocess.check_output(['git','-C',str(ROOT),*a])
def sha(b):return hashlib.sha256(b).hexdigest()
def main():
 E=D/'evidence';M=json.loads((D/'MANIFEST.json').read_text());assert M['source_commit']==S
 assert git('rev-list','--parents','-n','1',S).decode().split()==[S,B,N]
 assert all(p.startswith(('docs/','openspec/'))for p in git('diff','--name-only',S,'HEAD').decode().splitlines())
 assert not git('diff',B,S,'--','VERSION','cmd','go.mod','go.sum','scripts','deploy','tools/sessionhelper','view-service/renderer-canary-sse.html','internal/store/migrations')
 for p,h in M['files'].items():assert sha((ROOT/p).read_bytes())==sha(git('cat-file','blob','HEAD:'+p))==h,p
 for p,h in M['product_files'].items():assert sha((ROOT/p).read_bytes())==sha(git('cat-file','blob',S+':'+p))==sha(git('cat-file','blob','HEAD:'+p))==h,p
 for r in json.loads((E/'raw-logs.json').read_text()):
  b=(ROOT/r['archive_path']).read_bytes();assert sha(b)==r['archive_sha256'];assert sha(gzip.decompress(b))==r['published_sha256']
 inv=json.loads((E/'invariants.json').read_text());assert len(inv['merge_chain'])==9;assert all(inv['required_ancestors'].values())and all(inv['n3_exact_files'].values())and all(inv['unchanged_from_prior'].values())and inv['n1c_source_not_ancestor']
 g=json.loads((E/'gates.json').read_text());assert len(g)==5 and all(r['source']==S for r in g);assert all(r['rc']==0 for r in g if r['name']!='m8-store-full3')
 accept=json.loads((E/'gate-acceptance.json').read_text());assert accept['accepted'] and accept['matching_baselines_complete'];assert json.loads((E/'z3-admissibility-audit.json').read_text())['passed'];q=json.loads((E/'z3-qualification.json').read_text());assert q['complete']and q['passed']
 if q['required']:
  assert g[1]['rc']!=0 and len(q['rounds'])==5 and q['green_rounds']>=3
  allow={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses'}
  assert all(set(r['failures'])<=allow for r in q['rounds'])
 else:assert g[1]['rc']==0 and q['rounds']==[]
 receipt=(E/'browser-receipt.txt').read_text();suites=dict(re.findall(r'^suite\.([^.]+)\.rc=(\d+)$',receipt,re.M));assert len(suites)==48 and all(v=='0'for v in suites.values())and suites['history_provenance']=='0'
 pg=json.loads((E/'postgres-evidence.json').read_text());assert pg['versions']==['180003']and pg['full3_version_lines']>0
 profiles=json.loads((E/'helper-profile-results.json').read_text());assert len(profiles)==2 and all(p['actual']==p['expected'] and p['rc']==0 and p['files']==97 for p in profiles)
 c=json.loads((E/'compat-results.json').read_text());assert len(c)==3 and all(r['rc']==0 and r['actual_handshake']and r['actual_recv_accept']for r in c);assert [r['ack']['state']for r in c]==['busy','failed','busy'];assert [r['failure_notice_rows']for r in c]==[0,1,0]
 final=json.loads((E/'final-document-strict.json').read_text());assert final['rc']==0;assert sha((E/'final-document-strict.txt').read_bytes())==final['raw_sha256'];git('merge-base','--is-ancestor',final['document_commit'],'HEAD');assert not git('diff',final['document_commit'],'HEAD','--','openspec/changes/r61-integration')
 print('VERIFY_OK stage2-N1b-N3 source='+S+' merges9 browser48 compat3 z3_required='+str(q['required'])+' files='+str(len(M['files'])))
if __name__=='__main__':main()
