#!/usr/bin/env python3
"""离线核验第二段来源、产品等价、原日志、Z-3资格与真实进程结果。"""
from pathlib import Path
import subprocess,hashlib,json,gzip,re
ROOT=Path(__file__).resolve().parents[3];DOC=Path(__file__).resolve().parent
SOURCE='cd01d67a2c126c850b1229fc486ee30cb1ddd3e8';BASE='0ab43c630c37fdf01ce68d5c66c0db0141a682af';N1B='b81737d0897c7edfea171371dba8ead6c1ef8f28'
def git(*args):return subprocess.check_output(['git','-C',str(ROOT),*args])
def sha(b):return hashlib.sha256(b).hexdigest()
def main():
 e=DOC/'evidence';m=json.loads((DOC/'MANIFEST.json').read_text())
 assert git('rev-list','--parents','-n','1',SOURCE).decode().split()==[SOURCE,BASE,N1B]
 assert all(p.startswith(('docs/','openspec/'))for p in git('diff','--name-only',SOURCE,'HEAD').decode().splitlines())
 assert not git('diff',BASE,SOURCE,'--','VERSION','cmd','view-service','scripts','deploy','go.mod','go.sum')
 for p,h in m['files'].items():assert sha((ROOT/p).read_bytes())==sha(git('cat-file','blob','HEAD:'+p))==h,p
 for p,h in m['product_files'].items():assert sha((ROOT/p).read_bytes())==sha(git('cat-file','blob',SOURCE+':'+p))==sha(git('cat-file','blob','HEAD:'+p))==h,p
 for r in json.loads((e/'raw-logs.json').read_text()):
  b=(ROOT/r['archive_path']).read_bytes();assert sha(b)==r['archive_sha256'];assert sha(gzip.decompress(b))==r['published_sha256']
 g=json.loads((e/'gates.json').read_text());assert len(g)==2 and g[0]['rc']==0 and g[1]['rc']!=0
 q=json.loads((e/'z3-qualification.json').read_text());assert q['complete'] and q['passed'] and len(q['rounds'])==5 and q['green_rounds']>=3
 allow={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses'}
 for r in q['rounds']:assert set(r['failures'])<=allow
 follow=json.loads((e/'z3-followup-gates.json').read_text());assert len(follow)==9
 for r in follow:
  if r['name'] in ['focused-race','browser-full','strict']:assert r['rc']==0
 receipt=(e/'browser-receipt.txt').read_text();suites=dict(re.findall(r'^suite\.([^.]+)\.rc=(\d+)$',receipt,re.M));assert len(suites)==47 and all(v=='0'for v in suites.values())
 c=json.loads((e/'compat-results.json').read_text());assert len(c)==3 and all(r['rc']==0 and r['actual_handshake'] and r['actual_recv_accept']for r in c)
 assert [r['ack']['state']for r in c]==['busy','failed','busy'];assert [r['failure_notice_rows']for r in c]==[0,1,0]
 pg=json.loads((e/'postgres-evidence.json').read_text());assert pg['versions']==['180003'] and pg['full3_version_lines']>0
 final=json.loads((e/'final-document-strict.json').read_text());assert final['rc']==0;git('merge-base','--is-ancestor',final['document_commit'],'HEAD');assert not git('diff',final['document_commit'],'HEAD','--','openspec/changes/r61-integration')
 print('VERIFY_OK stage2 source='+SOURCE+' initial_FAIL_preserved Z3qualified5 compatibility3 browser47 evidence='+str(len(m['files'])))
if __name__=='__main__':main()
