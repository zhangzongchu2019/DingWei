from pathlib import Path
import subprocess,json,datetime,os
out=Path(__file__).parent;root=Path('/data/ai-dev-data/r61-integration');repo='git@git.code.tencent.com:wego-ai/dingwei.git'
items=[('togpt60lunazzc/helper-admission-flake','bfaec0ba27060299fc87b0ad592db9fb5dc75b63'),('togpt60lunazzc/helper-flake-2','f7b32525cd1b99d5188fbc3c8133018b6a8a8a82'),('togpt60lunazzc/r61-n1-busy-wait-v2','638cb32477af230bedd2aba0bc547dee82b162ef'),('qwen38mvb4/r61-n2-offturn-fold-v2','8ae6b083dc3c3875bc3ddf8cc6ae53fd593ab298'),('qwen38mvb4/browser-flake-windows-v2','415b9bf5199e711b5323e21a0a147082a2108498'),('qwen38mvb4/browser-followup','e26d7ee0046ef008cfa850a240d453869b56daa5'),('qwen38mvb4/browser-mutant-revive','429cf1dd9c0cb59cb82191d5c7cfb0014f2420e0')]
def run(*args):return subprocess.check_output(['git',*args],cwd=root,text=True)
records=json.loads((out/'merges.json').read_text()) if (out/'merges.json').exists() else []
assert not run('status','--porcelain').strip(),'resolve pending merge first'
for i,(branch,sha) in enumerate(items):
 if any(r.get('merged_sha') and r['source']==sha for r in records):continue
 actual=run('ls-remote',repo,'refs/heads/'+branch).strip();r=dict(index=i+1,branch=branch,source=sha,checked_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),ls_remote=actual,head_before=run('rev-parse','HEAD').strip());records.append(r);(out/'merges.json').write_text(json.dumps(records,indent=2)+'\n')
 if actual.split()[0]!=sha:raise SystemExit('TIP MISMATCH STOP '+branch)
 message=f'merge(r61): integrate stage1 item {i+1} {branch}\n\nSource: {sha}\nResolution: automatic merge; no manual conflict edits.\nScope: S-R61-INTEGRATION stage1; VERSION unchanged.'
 env=dict(os.environ)
 for key in ['GIT_AUTHOR_NAME','GIT_AUTHOR_EMAIL','GIT_COMMITTER_NAME','GIT_COMMITTER_EMAIL']:env.pop(key,None)
 cmd=['git','-c','user.name=coder56solzzc','-c','user.email=coder56solzzc@dingwei.local','merge','--no-ff',sha,'-m',message]
 result=subprocess.run(cmd,cwd=root,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True);r['rc']=result.returncode;(out/f'merge-{i+1}.log').write_text(result.stdout);print(i+1,result.returncode,result.stdout[-1500:],flush=True)
 if result.returncode:
  r['conflicts']=run('diff','--name-only','--diff-filter=U').splitlines();(out/'merges.json').write_text(json.dumps(records,indent=2)+'\n');raise SystemExit('CONFLICT STOP')
 r['merged_sha']=run('rev-parse','HEAD').strip();r['tree']=run('rev-parse','HEAD^{tree}').strip();(out/'merges.json').write_text(json.dumps(records,indent=2)+'\n')
