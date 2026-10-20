#!/usr/bin/env python3
"""Verify committed source/evidence bytes, never trust the current worktree."""
import argparse,hashlib,json,subprocess
BASE='docs/reviews/coder-feedback-finish-failure-20261020/'
def git(*args):return subprocess.check_output(['git',*args])
def paths(commit):
 out={}
 for record in git('ls-tree','-r','-z',commit).split(b'\0'):
  if not record:continue
  meta,path=record.split(b'\t',1);mode,kind,blob=meta.decode().split()
  if kind=='blob':out[path.decode()]={"blob":blob,"mode":mode}
 return out

def run(args):
 commit=git('rev-parse',args.commit+'^{commit}').decode().strip();tree=paths(commit)
 source=json.loads(git('show',commit+':'+BASE+'SOURCE-MANIFEST.json'));evidence=json.loads(git('show',commit+':'+BASE+'EVIDENCE-MANIFEST.json'))
 expected={p for p in tree if not p.startswith('docs/')};assert {x['path'] for x in source['entries']}==expected,'source path set differs'
 exclude={BASE+'EVIDENCE-MANIFEST.json',BASE+'FINAL-VERIFICATION.json'}
 expected={p for p in tree if p.startswith(BASE) and p not in exclude};assert {x['path'] for x in evidence['entries']}==expected,'evidence path set differs'
 count=0
 for group in [source,evidence]:
  for e in group['entries']:
   actual=tree[e['path']];assert actual['blob']==e['blob'] and actual['mode']==e['mode'],e['path']+' Git object differs'
   raw=git('cat-file','blob',actual['blob']);assert len(raw)==e['size'] and hashlib.sha256(raw).hexdigest()==e['sha256'],e['path']+' bytes differ';count+=1
 runtime=source['source_commit'];runtime_tree=paths(runtime)
 assert {p:v for p,v in runtime_tree.items() if not p.startswith('docs/')}=={p:v for p,v in tree.items() if not p.startswith('docs/')},'runtime changed after freeze'
 print(json.dumps({'verified_commit':commit,'runtime_source':runtime,'source_files':len(source['entries']),'evidence_files':len(evidence['entries']),'total_verified':count,'read_source':'Git blob, not worktree','deployment_authorized':False},indent=2))
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--commit',default='HEAD');run(p.parse_args())
