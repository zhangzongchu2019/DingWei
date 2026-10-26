from pathlib import Path
import subprocess,json,hashlib,datetime
root=Path('/data/ai-dev-data/r39-1-integration-02313-withdraw');out=Path('/data/ai-dev-data/r39-1-integration-02313-evidence')
files=[root/'VERSION',root/'internal/version/version.go',root/'tools/sessionhelper/sessionhelper/__init__.py'];lock=root/'tools/sessionhelper/requirements-runtime.lock';original={p:p.read_bytes() for p in files+[lock]}
env={'HOME':str(out/'withdraw-home'),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','GOTMPDIR':'/data/ai-dev-data/tmp-coder-go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','LANG':'C.UTF-8','TZ':'UTC'};Path(env['HOME']).mkdir(exist_ok=True);records=[]
def run(name):
 cmd=['/usr/bin/go','test','-p','1','-count=1','./internal/version','-v'];start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (out/(name+'.stdout')).open('wb') as so,(out/(name+'.stderr')).open('wb') as se:r=subprocess.run(cmd,cwd=root,env=env,stdout=so,stderr=se,timeout=120)
 d={'name':name,'command':cmd,'rc':r.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source_commit':'9f0c84d1204a78443cf2f29d3c4d3a799a79846e','sha256':{str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in original}};records.append(d);(out/(name+'.json')).write_text(json.dumps(d,indent=2)+'\n');print(json.dumps({'name':name,'rc':r.returncode}),flush=True);return r.returncode
try:
 for name,changed in [('M1-root-only',[files[0]]),('M2-go-only',[files[1]]),('M3-helper-only',[files[2]]),('M4-coherent-rollback',files),('M5-lock-byte-change',[lock])]:
  assert run(name+'-before')==0
  for p in changed:p.write_bytes(original[p]+b'\n' if p==lock else original[p].replace(b'0.2.3.13',b'0.2.3.12'))
  assert run(name+'-withdrawn')!=0
  for p in changed:p.write_bytes(original[p])
  assert run(name+'-restored')==0
finally:
 for p,s in original.items():p.write_bytes(s)
 (out/'withdraw-summary.json').write_text(json.dumps({'records':records,'restored':all(p.read_bytes()==s for p,s in original.items())},indent=2)+'\n')
