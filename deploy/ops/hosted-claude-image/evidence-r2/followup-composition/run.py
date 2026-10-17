from pathlib import Path
import subprocess,json,hashlib,time,datetime,tempfile,re
lab=Path(__file__).parent;wt=Path('/data/ai-dev-data/hosted-claude-packager');mutant=lab/'mutant';(lab/'home').mkdir(exist_ok=True)
env={'PATH':'/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/local/bin:/usr/bin:/bin','HOME':str(lab/'home'),'TMPDIR':tempfile.mkdtemp(prefix='cp-cmp-'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8','TZ':'UTC','GIT_CONFIG_NOSYSTEM':'1'}
(lab/'ENVIRONMENT.json').write_text(json.dumps(env,indent=2)+'\n');source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=wt,text=True).strip()
original=(lab/'reviewer-probes.original.py').read_text();oldpath='/tmp/ots-r2-be1/deploy/ops/hosted-claude-image/test_claude_start.py';newpath=str(mutant/'deploy/ops/hosted-claude-image/test_claude_start.py');assert original.count(oldpath)==1
adapted=original.replace(oldpath,newpath);assert adapted.replace(newpath,oldpath)==original
(lab/'reviewer-probes.py').write_text(adapted);(lab/'PROBE-PROVENANCE.json').write_text(json.dumps({'review':'d3b174ea663636a51aa5c0ce71568768891788e7','original_sha256':hashlib.sha256(original.encode()).hexdigest(),'adapted_sha256':hashlib.sha256(adapted.encode()).hexdigest(),'only_change':{'from':oldpath,'to':newpath},'assertions_unchanged':True},indent=2)+'\n')
results=[]
def run(label,args,cwd=wt,red=False):
 path=lab/'raw'/(label+'.log');t=time.monotonic();start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with path.open('w') as stream:p=subprocess.run(args,cwd=cwd,env=env,stdout=stream,stderr=subprocess.STDOUT)
 text=path.read_text();match=re.search(r'Ran (\d+) tests?',text);r={'source':source,'argv':args,'rc':p.returncode,'elapsed_seconds':round(time.monotonic()-t,3),'started_utc':start,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'tests':int(match.group(1)) if match else None,'semantic_assertion_red':'FAILED (failures=' in text and '\nFAIL:' in text,'errors':bool(re.search(r'FAILED \([^)]*errors=',text))}
 (lab/'raw'/(label+'.result.json')).write_text(json.dumps(r,indent=2)+'\n');results.append(r);print(label,json.dumps(r),flush=True)
 assert p.returncode==(1 if red else 0),(label,text[-2000:])
 if red:assert r['semantic_assertion_red'] and not r['errors']
 return r
base=['python3','-m','unittest','discover','-s','deploy/ops/hosted-claude-image','-p','test_claude_start.py']
run('claude-final',base+['-v'])
run('h3-regression',['python3','-m','unittest','discover','-s','deploy/ops/hosted-image','-p','test_*.py','-v'])
run('entrypoint-contract',['python3','deploy/test_build_entrypoints.py','-v'])
run('reviewer-original-contracts',['python3',str(lab/'reviewer-probes.py'),'-v'])
pc=mutant/'deploy/ops/hosted-claude-image/prepare_context.py';ignore=mutant/'deploy/ops/hosted-claude-image/dockerignore';before_pc=pc.read_text();before_ignore=ignore.read_text()
cases=[('ignore',ignore,'!start.py\n','','staged_copy_sources_survive_actual_ignore_rules'),('runtime-directories',pc,'os.fchmod(directory, 0o755)','os.fchmod(directory, 0o700)','runtime_uid10001_traversal'),('umask',pc,'os.fchmod(directory, 0o755)','pass  # withdrawn deterministic chmod','runtime_uid10001_traversal'),('package-modes',pc,'0o755 if member.mode & 0o111 else 0o644','member.mode & 0o755','archive_modes_are_readable')]
controls=[]
for name,path,old,new,pattern in cases:
 text=path.read_text();assert text.count(old)==1;changed=text.replace(old,new);item={'name':name,'file':str(path.relative_to(mutant)),'original_sha256':hashlib.sha256(text.encode()).hexdigest()}
 try:
  for phase in ['baseline','withdrawal','restored']:
   path.write_text(changed if phase=='withdrawal' else text)
   r=run('control-'+name+'-'+phase,base+['-k',pattern,'-v'],cwd=mutant,red=phase=='withdrawal');assert r['tests']==1;r['file_sha256']=hashlib.sha256(path.read_bytes()).hexdigest();item[phase]=r
 finally:path.write_text(text)
 controls.append(item)
# The exact old two product files reproduce both original reviewer assertions.
old_pc=subprocess.check_output(['git','show','be1dbb8d:deploy/ops/hosted-claude-image/prepare_context.py'],cwd=wt,text=True);old_ignore=subprocess.check_output(['git','show','be1dbb8d:deploy/ops/hosted-claude-image/dockerignore'],cwd=wt,text=True)
item={'name':'both-original-reviewer-findings','files':[str(pc.relative_to(mutant)),str(ignore.relative_to(mutant))]}
try:
 for phase in ['baseline','withdrawal','restored']:
  pc.write_text(old_pc if phase=='withdrawal' else before_pc);ignore.write_text(old_ignore if phase=='withdrawal' else before_ignore)
  r=run('control-original-'+phase,['python3',str(lab/'reviewer-probes.py'),'-v'],red=phase=='withdrawal');assert r['tests']==2;r['file_sha256']={str(p.relative_to(mutant)):hashlib.sha256(p.read_bytes()).hexdigest() for p in [pc,ignore]};item[phase]=r
finally:pc.write_text(before_pc);ignore.write_text(before_ignore)
controls.append(item)
(lab/'CONTROLS.json').write_text(json.dumps(controls,indent=2)+'\n');(lab/'RESULTS.json').write_text(json.dumps(results,indent=2)+'\n');print('COMPOSITION_LOCAL_GATES_OK',flush=True)
