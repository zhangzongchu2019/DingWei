from pathlib import Path
import subprocess,json,hashlib,re,time
lab=Path(__file__).parent;repo=lab/'mutant';env=json.loads((lab/'ENVIRONMENT.json').read_text());source='c84184fcf6db2d43cdce5d8ec95fba16f85d87ec';env={k:v for k,v in env.items() if k not in ['GOCACHE','GOPATH']};results=[]
p=repo/'deploy/ops/hosted-image/prepare_context.py';fixed=p.read_text();old=subprocess.check_output(['git','show','ed02adc7:deploy/ops/hosted-image/prepare_context.py'],cwd=repo,text=True)
base=['python3','-m','unittest','discover','-s','deploy/ops/hosted-image','-p','test_build_definition.py','-k','source_kit','-v']
def run(label,argv,red=False,errors=False):
 path=lab/'raw'/(label+'.log');t=time.monotonic()
 with path.open('w') as out:r=subprocess.run(argv,cwd=repo,env=env,stdout=out,stderr=subprocess.STDOUT)
 text=path.read_text();item={'label':label,'source':source,'argv':argv,'rc':r.returncode,'elapsed_seconds':round(time.monotonic()-t,3),'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'product_file_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'assertion_red':'\nFAIL:' in text and 'FAILED (failures=' in text,'tests':int(re.search(r'Ran (\d+) tests?',text)[1])};results.append(item);print(json.dumps(item),flush=True)
 assert r.returncode==(1 if red else 0),(label,text[-2000:])
 if red and not errors:assert item['assertion_red'] and 'errors=' not in text
 return item
try:
 # Preserve the exact previously reported plain-invocation regression separately.
 p.write_text(old);r=run('original-claude-no-env-reproduction',['python3','-m','unittest','discover','-s','deploy/ops/hosted-claude-image','-p','test_claude_start.py','-v'],red=True,errors=True);assert r['tests']==19
 p.write_text(fixed)
 for name,mutant in [('remove-load-guard',old),('remove-finally-restore',fixed.replace('sys.dont_write_bytecode = previous','pass  # withdrawn restoration'))]:
  assert mutant!=fixed
  for phase in ['baseline','withdrawal','restored']:
   p.write_text(mutant if phase=='withdrawal' else fixed);r=run(name+'-'+phase,base,red=phase=='withdrawal');assert r['tests']==2
finally:p.write_text(fixed)
(lab/'CONTROLS.json').write_text(json.dumps(results,indent=2)+'\n');print('BYTECODE_CONTROLS_OK',flush=True)
