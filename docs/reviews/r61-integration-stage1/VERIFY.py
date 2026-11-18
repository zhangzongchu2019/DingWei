#!/usr/bin/env python3
"""离线检查R61第一段七项坐标、源码同一性和日志证据，不启动服务。"""
from pathlib import Path
import subprocess,json,hashlib,gzip,re
ROOT=Path(__file__).resolve().parents[3];DOC=Path(__file__).resolve().parent
SOURCE='38b9538dd2b1f1783aee6d7890ea03b83e33184e';BASE='206d41b26dab646c0e50a111eed5a45ccf7c72f6'
def git(*a):return subprocess.check_output(['git','-C',str(ROOT),*a])
def sha(b):return hashlib.sha256(b).hexdigest()
def main():
 m=json.loads((DOC/'MANIFEST.json').read_text());assert m['source_commit']==SOURCE
 git('merge-base','--is-ancestor',BASE,SOURCE)
 changes=git('diff','--name-only',SOURCE,'HEAD').decode().splitlines();assert all(p.startswith(('docs/','openspec/')) for p in changes)
 assert not git('diff',BASE,SOURCE,'--','VERSION','internal/version.go','tools/sessionhelper/sessionhelper/__init__.py')
 for p,h in m['files'].items():assert sha((ROOT/p).read_bytes())==sha(git('cat-file','blob','HEAD:'+p))==h,p
 for p,h in m['product_files'].items():assert sha((ROOT/p).read_bytes())==sha(git('cat-file','blob',SOURCE+':'+p))==sha(git('cat-file','blob','HEAD:'+p))==h,p
 evidence=DOC/'evidence';merges=json.loads((evidence/'merges.json').read_text());assert len(merges)==7
 commits=git('rev-list','--first-parent','--reverse',BASE+'..'+SOURCE).decode().splitlines();assert commits==[r['merged_sha'] for r in merges]
 for i,r in enumerate(merges):
  assert r['index']==i+1 and r['rc']==0 and r['ls_remote'].split()[0]==r['source']
  parents=git('rev-list','--parents','-n','1',r['merged_sha']).decode().split();assert parents[1:]==[r['head_before'],r['source']]
  assert r['head_before']==(BASE if i==0 else merges[i-1]['merged_sha'])
  git('merge-base','--is-ancestor',r['source'],SOURCE)
 current=git('cat-file','blob',SOURCE+':view-service/console-browser-regression.js').decode();prior=git('cat-file','blob','429cf1dd:view-service/console-browser-regression.js').decode();white=lambda s:re.search(r'if \(MUTATION && !\[(.*?)\]\.includes\(MUTATION\)\)',s).group(1);assert white(current)==white(prior)
 for name in ['unhandled-group-action','legacy-group-deadlock','silent-group-action-feedback']:assert "if (MUTATION === '"+name+"')" in current
 gates=json.loads((evidence/'gates.json').read_text());assert len(gates)==5 and all(r['source_commit']==SOURCE and r['rc']==0 for r in gates)
 first=json.loads((evidence/'gates-enospc.json').read_text());assert first[0]['rc']==1
 for r in json.loads((evidence/'raw-logs.json').read_text()):
  b=(ROOT/r['archive_path']).read_bytes();assert sha(b)==r['archive_sha256'];assert sha(gzip.decompress(b))==r['published_sha256']
 guide=(evidence/'codex-guide-verdict.txt').read_text();assert 'profile=codex' in guide and 'test_claude_stream_json_user_frame_boundary_counts_zero_one_two' in guide and re.search(r'^ok\s*$',guide,re.M)
 receipt=(evidence/'browser-receipt.txt').read_text();suites=dict(re.findall(r'^suite\.([^.]+)\.rc=(\d+)$',receipt,re.M));assert suites and all(v=='0' for v in suites.values())
 for name in ['r15_session_history_pagination','r15_session_history_mutations']:assert suites[name]=='0'
 print('VERIFY_OK source='+SOURCE+' merges7 product_unchanged evidence_files='+str(len(m['files']))+' browser_suites='+str(len(suites)))
if __name__=='__main__':main()
