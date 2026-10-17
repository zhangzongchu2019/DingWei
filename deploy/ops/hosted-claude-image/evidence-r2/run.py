from pathlib import Path
import subprocess,json,hashlib,time,datetime,tempfile,re,sys
lab=Path(__file__).parent;wt=Path('/data/ai-dev-data/hosted-claude-packager');mutant=lab/'mutant'
for d in ['home','raw']:(lab/d).mkdir(exist_ok=True)
env={'PATH':'/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/local/bin:/usr/bin:/bin','HOME':str(lab/'home'),'TMPDIR':tempfile.mkdtemp(prefix='cp-r2-'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8','TZ':'UTC','GIT_CONFIG_NOSYSTEM':'1'}
(lab/'ENVIRONMENT.json').write_text(json.dumps(env,indent=2)+'\n');source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=wt,text=True).strip()
results=[]
def run(label,args,cwd=wt,red=False):
 log=lab/'raw'/(label+'.log');start=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic()
 with log.open('w') as f:p=subprocess.run(args,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT)
 data=log.read_text();match=re.search(r'Ran (\d+) tests?',data);result={'source':source,'argv':args,'rc':p.returncode,'elapsed_seconds':round(time.monotonic()-t,3),'started_utc':start,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':hashlib.sha256(log.read_bytes()).hexdigest(),'tests':int(match.group(1)) if match else None,'semantic_assertion_red':'FAILED (failures=' in data and '\nFAIL:' in data,'errors':'FAILED (errors=' in data}
 (lab/'raw'/(label+'.result.json')).write_text(json.dumps(result,indent=2)+'\n');results.append(result);print(label,json.dumps(result),flush=True)
 assert p.returncode==(1 if red else 0),(label,data[-2500:])
 if red:assert result['semantic_assertion_red'] and not result['errors'],(label,data[-2500:])
 return result
base=['python3','-m','unittest','discover']
run('claude-final',base+['-s','deploy/ops/hosted-claude-image','-p','test_claude_start.py','-v'])
run('h3-regression',base+['-s','deploy/ops/hosted-image','-p','test_*.py','-v'])
run('entrypoint-contract',['python3','deploy/test_build_entrypoints.py','-v'])
run('shell-syntax',['sh','-n','deploy/ops/hosted-claude-image/build-image.sh'])
file=mutant/'deploy/ops/hosted-claude-image/prepare_context.py';original=file.read_text();shell=mutant/'deploy/ops/hosted-claude-image/build-image.sh';shell_original=shell.read_text()
cases=[
 ('git-content',file,'files[name][0] != content','False','h3_git_or_locked_content'),
 ('receipt',file,"{n: row[1] for n, row in files.items()} != receipt['files']",'False','missing_extra_or_receipt_pollution'),
 ('wheel-lock',file,'digest not in pins[dist][1]','False','h3_git_or_locked_content'),
 ('deb-lock',file,"files[n][1] != digest or files[n][2] != 0o644",'False','h3_git_or_locked_content'),
 ('nofollow',file,'os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK','os.O_RDONLY | os.O_NONBLOCK','regular_symlink_is_rejected'),
 ('overlay',file,"selected.update({name: (data, 0o644) for name, data in overlays.items()})","selected.update({name: (data, 0o644) for name, data in overlays.items()})\n    selected['Dockerfile'] = (inputs['Dockerfile'][0], 0o644)",'actual_h3_shape_composes'),
 ('docker-closed',shell,'exit 2\n','exit 0\n','pre_docker_script_remains_closed'),
]
controls=[]
for name,path,old,new,pattern in cases:
 text=path.read_text()
 # docker-closed targets only the final unconditional refusal.
 if name=='docker-closed':changed=text.rsplit(old,1);assert len(changed)==2;changed=new.join(changed)
 else:assert text.count(old)==1,(name,text.count(old));changed=text.replace(old,new)
 record={'name':name,'file':str(path.relative_to(mutant)),'original_sha256':hashlib.sha256(text.encode()).hexdigest()}
 try:
  for phase in ['baseline','withdrawal','restored']:
   path.write_text(changed if phase=='withdrawal' else text)
   r=run('control-'+name+'-'+phase,base+['-s','deploy/ops/hosted-claude-image','-p','test_claude_start.py','-k',pattern,'-v'],cwd=mutant,red=phase=='withdrawal')
   assert r['tests']==1,r
   record[phase]=r
 finally:path.write_text(text)
 controls.append(record);(lab/('control-'+name+'.json')).write_text(json.dumps(record,indent=2)+'\n')
assert file.read_text()==original and shell.read_text()==shell_original
(lab/'CONTROLS.json').write_text(json.dumps(controls,indent=2)+'\n');(lab/'RESULTS.json').write_text(json.dumps(results,indent=2)+'\n')
print('R2_LOCAL_GATES_OK',flush=True)
